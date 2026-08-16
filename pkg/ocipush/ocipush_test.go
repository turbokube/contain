package ocipush_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/turbokube/contain/pkg/ocipush"
)

// TestPushSingleEntryLayout pushes a buildx-style layout (index.json with one
// entry) to a plain registry without the direct-upload extension, so the
// client must fall back to standard OCI uploads.
func TestPushSingleEntryLayout(t *testing.T) {
	dir, img := layoutWithImage(t, 1024, 2)

	// A plain registry must never receive extension-path requests: detection
	// is discovery-only, and this registry does not advertise _directpush.
	var extRequests atomic.Int32
	reg := quietRegistry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "_directpush") {
			extRequests.Add(1)
		}
		reg.ServeHTTP(w, r)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	image := host + "/test/single:v1"

	// ExtThreshold 1 would use the extension for every blob if advertised.
	err := ocipush.Push(context.Background(), dir, image, ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1,
	})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if n := extRequests.Load(); n != 0 {
		t.Errorf("plain registry received %d _directpush requests, want 0", n)
	}

	assertPushedDigest(t, image, img)
}

// TestPushMultiEntryLayout pushes a layout whose index.json has several
// entries; the index.json content itself becomes the root manifest and its
// digest must be preserved.
func TestPushMultiEntryLayout(t *testing.T) {
	ii, err := random.Index(1024, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := layout.Write(dir, ii); err != nil {
		t.Fatal(err)
	}
	indexBytes, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("sha256:%x", sha256.Sum256(indexBytes))

	server := httptest.NewServer(quietRegistry())
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	image := host + "/test/multi:v1"

	if err := ocipush.Push(context.Background(), dir, image, ocipush.Options{Auth: authn.Anonymous}); err != nil {
		t.Fatalf("push: %v", err)
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remote.Get(ref, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull after push: %v", err)
	}
	if got.Digest.String() != want {
		t.Errorf("index digest not preserved: index.json %s got %s", want, got.Digest)
	}
	idx, err := remote.Index(ref, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("as index: %v", err)
	}
	im, err := idx.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(im.Manifests) != 2 {
		t.Errorf("expected 2 manifests in pushed index, got %d", len(im.Manifests))
	}
}

// extFake wraps a plain registry with the direct-upload extension endpoints,
// mimicking the Worker: presigned part URLs, staged parts, commit with
// server-side digest verification before promotion to the blob store.
type extFake struct {
	reg       http.Handler
	serverURL string
	mu        sync.Mutex
	staged    map[string]map[int][]byte // key -> part number -> bytes
	uploads   int
	commits   int
	// expiresSeconds is advertised on the session; 0 omits the field.
	expiresSeconds int64
	// extraHeaders are prescribed on top of the checksum header, to stand in
	// for the header set a presigned signature covers.
	extraHeaders map[string]string
}

const fakePartSize = int64(1000)

// extDiscoverDocument is what a registry advertising the extension serves;
// shared so the fakes cannot drift from each other.
const extDiscoverDocument = `{"extensions":[{"name":"_directpush","endpoints":["_directpush/v1/uploads","_directpush/v1/commit"]}]}`

func (f *extFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v2/_oci/ext/discover":
		json.NewEncoder(w).Encode(map[string]any{
			"extensions": []map[string]any{{
				"name":      "_directpush",
				"endpoints": []string{"_directpush/v1/uploads", "_directpush/v1/commit"},
			}},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v2/_directpush/v1/uploads":
		var body struct {
			Repository string `json:"repository"`
			Digest     string `json:"digest"`
			Size       int64  `json:"size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if body.Repository == "" {
			// Models a registry with per-repository authorization, which MUST
			// reject the absence of the field. It is SHOULD-send in v1, so
			// this pins our client's behaviour rather than the protocol's:
			// always sending it is what keeps a client compatible with both
			// kinds of registry, and with a future v2 that requires it.
			http.Error(w, "repository field required by this registry", 400)
			return
		}
		key := "staging/" + body.Digest + "/fake"
		parts := int((body.Size + fakePartSize - 1) / fakePartSize)
		urls := make([]string, parts)
		for i := range urls {
			urls[i] = fmt.Sprintf("%s/stage/%s?part=%d", f.serverURL, key, i+1)
		}
		uploadID := ""
		if parts > 1 {
			uploadID = "fake-multipart"
		}
		f.mu.Lock()
		f.staged[key] = map[int][]byte{}
		f.uploads++
		f.mu.Unlock()
		headers := map[string]string{"x-contain-test-checksum": body.Digest}
		for k, v := range f.extraHeaders {
			headers[k] = v
		}
		session := map[string]any{
			"digest": body.Digest, "key": key, "uploadId": uploadID,
			"partSize": fakePartSize, "urls": urls,
			"headers": headers,
		}
		if f.expiresSeconds > 0 {
			session["expiresSeconds"] = f.expiresSeconds
		}
		json.NewEncoder(w).Encode(session)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/stage/"):
		// session-prescribed headers must arrive on every part PUT
		if !strings.HasPrefix(r.Header.Get("x-contain-test-checksum"), "sha256:") {
			http.Error(w, "missing session header", 400)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/stage/")
		var part int
		fmt.Sscanf(r.URL.Query().Get("part"), "%d", &part)
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.staged[key][part] = b
		f.mu.Unlock()
		w.Header().Set("ETag", fmt.Sprintf("%q", fmt.Sprintf("part%d", part)))
		w.WriteHeader(200)
	case r.Method == http.MethodPost && r.URL.Path == "/v2/_directpush/v1/commit":
		var body struct {
			Repository string `json:"repository"`
			Digest     string `json:"digest"`
			Size       int64  `json:"size"`
			Key        string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if body.Repository == "" {
			http.Error(w, "repository field required by this registry", 400)
			return
		}
		f.mu.Lock()
		parts := f.staged[body.Key]
		var blob []byte
		for i := 1; i <= len(parts); i++ {
			blob = append(blob, parts[i]...)
		}
		f.commits++
		f.mu.Unlock()
		if actual := fmt.Sprintf("sha256:%x", sha256.Sum256(blob)); actual != body.Digest {
			http.Error(w, "digest mismatch: "+actual, 400)
			return
		}
		if int64(len(blob)) != body.Size {
			http.Error(w, "size mismatch", 400)
			return
		}
		if err := f.promote(blob, body.Digest); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "digest": body.Digest})
	default:
		f.reg.ServeHTTP(w, r)
	}
}

// promote pushes verified bytes into the wrapped registry's (global) blob
// store via a synthesized standard upload.
func (f *extFake) promote(blob []byte, digest string) error {
	post := httptest.NewRequest(http.MethodPost, "/v2/staging/blobs/uploads/", nil)
	rec := httptest.NewRecorder()
	f.reg.ServeHTTP(rec, post)
	if rec.Code != http.StatusAccepted {
		return fmt.Errorf("promote start: %d", rec.Code)
	}
	location := rec.Result().Header.Get("Location")
	sep := "?"
	if strings.Contains(location, "?") {
		sep = "&"
	}
	put := httptest.NewRequest(http.MethodPut, location+sep+"digest="+digest, strings.NewReader(string(blob)))
	rec = httptest.NewRecorder()
	f.reg.ServeHTTP(rec, put)
	if rec.Code != http.StatusCreated {
		return fmt.Errorf("promote put: %d", rec.Code)
	}
	return nil
}

// TestPushExt pushes through the extension API with multipart staging and
// verifies both the resulting digests and that the extension was used.
func TestPushExt(t *testing.T) {
	dir, img := layoutWithImage(t, 3500, 2)

	fake := &extFake{reg: quietRegistry(), staged: map[string]map[int][]byte{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	fake.serverURL = server.URL
	host := strings.TrimPrefix(server.URL, "http://")
	image := host + "/test/ext:v1"

	err := ocipush.Push(context.Background(), dir, image, ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1, // everything through the extension
	})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if fake.uploads == 0 || fake.commits == 0 {
		t.Errorf("extension not exercised: %d uploads %d commits", fake.uploads, fake.commits)
	}

	assertPushedDigest(t, image, img)
	// and the layer bytes, so correct digests over missing content still fails
	assertLayersReadable(t, image)
}

// TestPushRejectsInconsistentLayout: the root manifest goes up by tag, so a
// layout whose bytes do not match the descriptor digest would otherwise be
// published under that tag with the registry none the wiser.
func TestPushRejectsInconsistentLayout(t *testing.T) {
	dir, _ := layoutWithImage(t, 1024, 1)

	// corrupt the blob the layout index points at, keeping the file name
	indexBytes, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(indexBytes, &idx); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(dir, "blobs", "sha256", strings.TrimPrefix(idx.Manifests[0].Digest, "sha256:"))
	original, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPath, append(original, ' '), 0o644); err != nil {
		t.Fatal(err)
	}

	var pushes atomic.Int32
	reg := quietRegistry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			pushes.Add(1)
		}
		reg.ServeHTTP(w, r)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	err = ocipush.Push(context.Background(), dir, host+"/test/corrupt:v1",
		ocipush.Options{Auth: authn.Anonymous})
	if err == nil {
		t.Fatal("expected a push failure for a layout that does not match its digests")
	}
	if !strings.Contains(err.Error(), "hashes to") {
		t.Errorf("unexpected error: %v", err)
	}
	if n := pushes.Load(); n != 0 {
		t.Errorf("%d write requests reached the registry, want 0", n)
	}
}

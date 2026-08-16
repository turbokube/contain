package ocipush_test

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/turbokube/contain/pkg/ocipush"
)

// expiringFake answers part PUTs with 403 until the client has taken
// expireSessions fresh sessions, which is how object storage reports a
// presigned URL past its lifetime. Sessions are not resumable, so the client
// must re-POST /uploads and re-upload every part rather than retrying the
// individual PUT.
type expiringFake struct {
	*extFake
	mu             sync.Mutex
	sessions       int
	expireSessions int
	partAttempts   int
}

func (f *expiringFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v2/_directpush/v1/uploads":
		f.mu.Lock()
		f.sessions++
		f.mu.Unlock()
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/stage/"):
		f.mu.Lock()
		f.partAttempts++
		expired := f.sessions <= f.expireSessions
		f.mu.Unlock()
		if expired {
			io.Copy(io.Discard, r.Body) //nolint:errcheck
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Request has expired</Message></Error>`)) //nolint:errcheck
			return
		}
	}
	f.extFake.ServeHTTP(w, r)
}

// TestPushExtRestartsExpiredSession covers the expiry contract added to
// PROTOCOL.md: on a 403 from storage the client takes a fresh session for the
// same digest and re-uploads all parts.
func TestPushExtRestartsExpiredSession(t *testing.T) {
	img, err := random.Image(3500, 2) // layers > fakePartSize, so multipart
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ii := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	if _, err := layout.Write(dir, ii); err != nil {
		t.Fatal(err)
	}

	fake := &expiringFake{
		extFake: &extFake{
			reg:            registry.New(registry.Logger(log.New(io.Discard, "", 0))),
			staged:         map[string]map[int][]byte{},
			expiresSeconds: 900,
		},
		expireSessions: 1, // the first session's URLs are already expired
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	fake.serverURL = server.URL
	host := strings.TrimPrefix(server.URL, "http://")
	image := host + "/test/expiry:v1"

	err = ocipush.Push(context.Background(), dir, image, ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1,
	})
	if err != nil {
		t.Fatalf("push should survive one session expiry: %v", err)
	}
	fake.mu.Lock()
	sessions := fake.sessions
	fake.mu.Unlock()
	if sessions < 2 {
		t.Errorf("expected a fresh session after expiry, got %d sessions", sessions)
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remote.Get(ref, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull after push: %v", err)
	}
	want, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest.String() != want.String() {
		t.Errorf("digest not preserved across the retry: pushed %s got %s", want, got.Digest)
	}
}

// TestPushExtGivesUpAfterRepeatedExpiry: retries are bounded. A registry whose
// window is too short for the blob must surface an error, not loop.
func TestPushExtGivesUpAfterRepeatedExpiry(t *testing.T) {
	img, err := random.Image(3500, 1)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ii := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	if _, err := layout.Write(dir, ii); err != nil {
		t.Fatal(err)
	}

	fake := &expiringFake{
		extFake: &extFake{
			reg:            registry.New(registry.Logger(log.New(io.Discard, "", 0))),
			staged:         map[string]map[int][]byte{},
			expiresSeconds: 1,
		},
		expireSessions: 99, // every session expires
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	fake.serverURL = server.URL
	host := strings.TrimPrefix(server.URL, "http://")

	err = ocipush.Push(context.Background(), dir, host+"/test/alwaysexpired:v1", ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1,
	})
	if err == nil {
		t.Fatal("expected an error when every session expires")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error should name the expiry, got: %v", err)
	}
	fake.mu.Lock()
	sessions := fake.sessions
	fake.mu.Unlock()
	if sessions > 4 {
		t.Errorf("retries are not bounded: %d sessions", sessions)
	}
}

// headerRecordingFake records exactly what arrived on part PUTs.
type headerRecordingFake struct {
	*extFake
	mu      sync.Mutex
	headers []http.Header
}

func (f *headerRecordingFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/stage/") {
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
	}
	f.extFake.ServeHTTP(w, r)
}

// TestPushExtPartHeaderDiscipline: presigned signatures cover a fixed header
// set, so PROTOCOL.md requires every prescribed header verbatim, an exact
// Content-Length, and nothing else the signature would not expect — notably no
// Content-Type and no unprescribed x-amz-*.
func TestPushExtPartHeaderDiscipline(t *testing.T) {
	img, err := random.Image(3500, 1)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ii := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	if _, err := layout.Write(dir, ii); err != nil {
		t.Fatal(err)
	}

	prescribed := map[string]string{
		"x-amz-checksum-sha256": "sentinel-checksum",
		"x-contain-test-extra":  "prescribed-value",
	}
	fake := &headerRecordingFake{
		extFake: &extFake{
			reg:            registry.New(registry.Logger(log.New(io.Discard, "", 0))),
			staged:         map[string]map[int][]byte{},
			extraHeaders:   prescribed,
			expiresSeconds: 900,
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	fake.serverURL = server.URL
	host := strings.TrimPrefix(server.URL, "http://")

	err = ocipush.Push(context.Background(), dir, host+"/test/headers:v1", ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1,
	})
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	fake.mu.Lock()
	recorded := fake.headers
	fake.mu.Unlock()
	if len(recorded) == 0 {
		t.Fatal("no part uploads recorded")
	}
	for i, h := range recorded {
		if h.Get("x-contain-test-extra") != "prescribed-value" {
			t.Errorf("part %d: prescribed header not sent verbatim: %q", i, h.Get("x-contain-test-extra"))
		}
		if !strings.HasPrefix(h.Get("x-contain-test-checksum"), "sha256:") {
			t.Errorf("part %d: session checksum header missing", i)
		}
		if h.Get("x-amz-checksum-sha256") != "sentinel-checksum" {
			t.Errorf("part %d: prescribed x-amz header not verbatim: %q", i, h.Get("x-amz-checksum-sha256"))
		}
		if ct := h.Get("Content-Type"); ct != "" {
			t.Errorf("part %d: Content-Type %q would invalidate the signature on some backends", i, ct)
		}
		if h.Get("Content-Length") == "" && h.Get("Transfer-Encoding") != "" {
			t.Errorf("part %d: chunked encoding instead of an exact Content-Length", i)
		}
		if auth := h.Get("Authorization"); auth != "" {
			t.Errorf("part %d: registry credentials sent to a presigned url: %q", i, auth)
		}
		for name := range h {
			lower := strings.ToLower(name)
			if !strings.HasPrefix(lower, "x-amz-") {
				continue
			}
			if _, ok := prescribed[lower]; !ok {
				t.Errorf("part %d: unprescribed %s breaks the presigned signature", i, name)
			}
		}
	}
}

// notConfiguredFake advertises _directpush from a static discovery document
// but answers every session request 501, which is what a registry instance
// missing its object-storage credentials does: discovery is a capability
// list, not a health check.
type notConfiguredFake struct {
	reg      http.Handler
	sessions atomic.Int32
	standard atomic.Int32
}

func (f *notConfiguredFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v2/_oci/ext/discover":
		w.Write([]byte(`{"extensions":[{"name":"_directpush","endpoints":["_directpush/v1/uploads","_directpush/v1/commit"]}]}`)) //nolint:errcheck
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v2/_directpush/"):
		f.sessions.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"errors":[{"code":"UNSUPPORTED","message":"direct upload not available"}]}`)) //nolint:errcheck
	default:
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/") {
			f.standard.Add(1)
		}
		f.reg.ServeHTTP(w, r)
	}
}

// TestPushExtFallsBackWhenAdvertisedButUnavailable: a 501 from the session
// endpoint must not fail the push. Standard OCI upload is still correct, and
// the extension is disabled for the rest of the process rather than retried
// per blob.
func TestPushExtFallsBackWhenAdvertisedButUnavailable(t *testing.T) {
	img, err := random.Image(3500, 3)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ii := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	if _, err := layout.Write(dir, ii); err != nil {
		t.Fatal(err)
	}

	fake := &notConfiguredFake{reg: registry.New(registry.Logger(log.New(io.Discard, "", 0)))}
	server := httptest.NewServer(fake)
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	image := host + "/test/notconfigured:v1"

	err = ocipush.Push(context.Background(), dir, image, ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1, // every blob would prefer the extension
	})
	if err != nil {
		t.Fatalf("push must fall back to standard upload, got: %v", err)
	}
	if n := fake.sessions.Load(); n != 1 {
		t.Errorf("session endpoint called %d times, want 1 before the extension is disabled", n)
	}
	if fake.standard.Load() == 0 {
		t.Error("no standard uploads were made")
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remote.Get(ref, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull after push: %v", err)
	}
	want, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest.String() != want.String() {
		t.Errorf("digest not preserved: pushed %s got %s", want, got.Digest)
	}
}

func TestNewProxyRejectsInvalidPrefix(t *testing.T) {
	for _, prefix := range []string{"Team/", "../evil/", "team//", "-team/"} {
		if _, err := ocipush.NewProxy("registry.example.com", prefix, ocipush.Options{Auth: authn.Anonymous}); err == nil {
			t.Errorf("prefix %q should be rejected: it becomes part of the upstream repository name", prefix)
		}
	}
	for _, prefix := range []string{"", "team/", "team/sub/", "my-team_1/"} {
		if _, err := ocipush.NewProxy("registry.example.com", prefix, ocipush.Options{Auth: authn.Anonymous}); err != nil {
			t.Errorf("prefix %q should be accepted: %v", prefix, err)
		}
	}
}

// emptyExtensionsFake answers discovery 200 with an empty extensions list,
// which is what a registry that implements _directpush but cannot currently
// perform it MUST now serve instead of advertising an endpoint that would
// 501 (PROTOCOL.md, "Discovery").
type emptyExtensionsFake struct {
	reg      http.Handler
	discover atomic.Int32
	extPaths atomic.Int32
}

func (f *emptyExtensionsFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v2/_oci/ext/discover":
		f.discover.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"extensions":[]}`)) //nolint:errcheck
	case strings.Contains(r.URL.Path, "_directpush"):
		f.extPaths.Add(1)
		http.Error(w, "must not be called", http.StatusNotImplemented)
	default:
		f.reg.ServeHTTP(w, r)
	}
}

// TestPushEmptyExtensionsListMeansNo distinguishes "this registry does not
// advertise the extension" from the 404 that a registry with no discovery
// endpoint gives: an empty list is a valid 200 with parseable JSON, so the
// decision has to come from the list contents rather than the status code.
func TestPushEmptyExtensionsListMeansNo(t *testing.T) {
	img, err := random.Image(3500, 2)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ii := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	if _, err := layout.Write(dir, ii); err != nil {
		t.Fatal(err)
	}

	fake := &emptyExtensionsFake{reg: registry.New(registry.Logger(log.New(io.Discard, "", 0)))}
	server := httptest.NewServer(fake)
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	image := host + "/test/emptyext:v1"

	err = ocipush.Push(context.Background(), dir, image, ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1, // every blob would prefer the extension if advertised
	})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if n := fake.extPaths.Load(); n != 0 {
		t.Errorf("%d extension requests to a registry that advertises nothing, want 0", n)
	}
	if n := fake.discover.Load(); n != 1 {
		t.Errorf("discovery called %d times, want 1 cached lookup", n)
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remote.Get(ref, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull after push: %v", err)
	}
	want, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest.String() != want.String() {
		t.Errorf("digest not preserved: pushed %s got %s", want, got.Digest)
	}
}

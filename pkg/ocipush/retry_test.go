package ocipush_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/turbokube/contain/pkg/ocipush"
)

// flakyRegistry fails the first failBlobPuts blob PUTs the way a dropped
// connection or an overloaded registry does, then behaves.
type flakyRegistry struct {
	reg          http.Handler
	failBlobPuts int32
	status       int
	blobPuts     atomic.Int32
	failed       atomic.Int32
}

func (f *flakyRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/") {
		if f.blobPuts.Add(1) <= f.failBlobPuts {
			f.failed.Add(1)
			w.WriteHeader(f.status)
			return
		}
	}
	f.reg.ServeHTTP(w, r)
}

// A monolithic blob upload is the whole layer in one request, so losing it
// to a transient failure used to fail the entire push. It is retried now.
func TestStandardUploadRetriesTransientFailure(t *testing.T) {
	dir, img := layoutWithImage(t, 2048, 2)

	fake := &flakyRegistry{reg: quietRegistry(), failBlobPuts: 2, status: http.StatusServiceUnavailable}
	server := httptest.NewServer(fake)
	defer server.Close()
	image := hostOf(server) + "/test/flaky:v1"

	err := ocipush.Push(context.Background(), dir, image, ocipush.Options{Auth: authn.Anonymous})
	if err != nil {
		t.Fatalf("push should survive transient upload failures: %v", err)
	}
	if fake.failed.Load() == 0 {
		t.Error("no failure was injected, the test proves nothing")
	}
	assertPushedDigest(t, image, img)
	assertLayersReadable(t, image)
}

// A 4xx means the registry understood the request and refused it. Resending
// the same bytes cannot help, and for a multi-GB layer it is expensive to
// find that out three times.
func TestStandardUploadDoesNotRetryClientError(t *testing.T) {
	dir, _ := layoutWithImage(t, 2048, 1)

	fake := &flakyRegistry{reg: quietRegistry(), failBlobPuts: 99, status: http.StatusBadRequest}
	server := httptest.NewServer(fake)
	defer server.Close()

	err := ocipush.Push(context.Background(), dir, hostOf(server)+"/test/refused:v1",
		ocipush.Options{Auth: authn.Anonymous})
	if err == nil {
		t.Fatal("expected the push to fail")
	}
	if n := fake.blobPuts.Load(); n != 1 {
		t.Errorf("blob PUT attempted %d times for a 4xx, want 1", n)
	}
}

// Retries are bounded: a registry that is down stays down, and the error
// has to surface rather than loop.
func TestStandardUploadRetriesAreBounded(t *testing.T) {
	dir, _ := layoutWithImage(t, 2048, 1)

	fake := &flakyRegistry{reg: quietRegistry(), failBlobPuts: 99, status: http.StatusServiceUnavailable}
	server := httptest.NewServer(fake)
	defer server.Close()

	err := ocipush.Push(context.Background(), dir, hostOf(server)+"/test/down:v1",
		ocipush.Options{Auth: authn.Anonymous})
	if err == nil {
		t.Fatal("expected the push to fail")
	}
	if n := fake.blobPuts.Load(); n > 4 {
		t.Errorf("blob PUT attempted %d times, retries are not bounded", n)
	}
}

// flakyManifests fails the first failPuts manifest PUTs with status and body.
type flakyManifests struct {
	reg      http.Handler
	failPuts int32
	status   int
	body     string
	puts     atomic.Int32
	failed   atomic.Int32
}

func (f *flakyManifests) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/manifests/") {
		if f.puts.Add(1) <= f.failPuts {
			f.failed.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(f.status)
			w.Write([]byte(f.body)) //nolint:errcheck
			return
		}
	}
	f.reg.ServeHTTP(w, r)
}

// What distribution (gcs driver) answered when GCS returned 503 to the stat
// of a blob that was in fact there: the blob-unknown code in the body made
// go-containerregistry's predicate give up, and contain's own client never
// retried manifests at all.
const mixedCodesBody = `{"errors":[{"code":"UNKNOWN","message":"unknown error"},` +
	`{"code":"UNKNOWN","message":"unknown error"},` +
	`{"code":"MANIFEST_BLOB_UNKNOWN","message":"blob unknown to registry"}]}`

func TestManifestPutRetriesServerError(t *testing.T) {
	dir, img := layoutWithImage(t, 2048, 1)

	fake := &flakyManifests{reg: quietRegistry(), failPuts: 2,
		status: http.StatusInternalServerError, body: mixedCodesBody}
	server := httptest.NewServer(fake)
	defer server.Close()
	image := hostOf(server) + "/test/manifest-flaky:v1"

	err := ocipush.Push(context.Background(), dir, image, ocipush.Options{Auth: authn.Anonymous})
	if err != nil {
		t.Fatalf("push should survive transient manifest failures: %v", err)
	}
	if fake.failed.Load() == 0 {
		t.Error("no failure was injected, the test proves nothing")
	}
	assertPushedDigest(t, image, img)
}

// distribution answers a manifest referencing a blob it really lacks with 400.
func TestManifestPutDoesNotRetryClientError(t *testing.T) {
	dir, _ := layoutWithImage(t, 2048, 1)

	fake := &flakyManifests{reg: quietRegistry(), failPuts: 99, status: http.StatusBadRequest,
		body: `{"errors":[{"code":"MANIFEST_BLOB_UNKNOWN","message":"blob unknown to registry"}]}`}
	server := httptest.NewServer(fake)
	defer server.Close()

	err := ocipush.Push(context.Background(), dir, hostOf(server)+"/test/manifest-refused:v1",
		ocipush.Options{Auth: authn.Anonymous})
	if err == nil {
		t.Fatal("expected the push to fail")
	}
	if n := fake.puts.Load(); n != 1 {
		t.Errorf("manifest PUT attempted %d times for a 4xx, want 1", n)
	}
}

func TestManifestPutRetriesAreBounded(t *testing.T) {
	dir, _ := layoutWithImage(t, 2048, 1)

	fake := &flakyManifests{reg: quietRegistry(), failPuts: 99,
		status: http.StatusServiceUnavailable, body: `{"errors":[{"code":"UNAVAILABLE"}]}`}
	server := httptest.NewServer(fake)
	defer server.Close()

	err := ocipush.Push(context.Background(), dir, hostOf(server)+"/test/manifest-down:v1",
		ocipush.Options{Auth: authn.Anonymous})
	if err == nil {
		t.Fatal("expected the push to fail")
	}
	if !strings.Contains(err.Error(), "after 4 attempts") {
		t.Errorf("error should say how many attempts were made: %v", err)
	}
	if n := fake.puts.Load(); n != 4 {
		t.Errorf("manifest PUT attempted %d times, want 4", n)
	}
}

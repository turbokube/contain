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

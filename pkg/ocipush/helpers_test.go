package ocipush_test

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// quietRegistry is a real in-memory OCI registry with its logging silenced.
func quietRegistry() http.Handler {
	return registry.New(registry.Logger(log.New(io.Discard, "", 0)))
}

// hostOf is the host:port of a test server, the form image references take.
func hostOf(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

// layoutWithImage writes a single-entry OCI layout, the shape
// `docker buildx build -o type=oci` produces, and returns its directory
// alongside the image so a test can assert the digest survived the push.
// layerSize above the fake part size is what makes the extension fakes take
// their multipart path.
func layoutWithImage(t *testing.T, layerSize int64, layers int) (string, v1.Image) {
	t.Helper()
	img, err := random.Image(layerSize, int64(layers))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ii := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	if _, err := layout.Write(dir, ii); err != nil {
		t.Fatal(err)
	}
	return dir, img
}

// assertPushedDigest pulls image back and fails unless it carries want's
// digest, which is the whole point of this package: bytes in, same bytes out.
func assertPushedDigest(t *testing.T, image string, want v1.Image) {
	t.Helper()
	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remote.Get(ref, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull after push: %v", err)
	}
	wantDigest, err := want.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest.String() != wantDigest.String() {
		t.Errorf("digest not preserved: pushed %s got %s", wantDigest, got.Digest)
	}
}

// assertLayersReadable pulls every layer and drains it, so a push that
// recorded correct digests but stored wrong or missing bytes still fails.
func assertLayersReadable(t *testing.T, image string) {
	t.Helper()
	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatal(err)
	}
	img, err := remote.Image(ref, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatal(err)
	}
	layers, err := img.Layers()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range layers {
		rc, err := l.Compressed()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			t.Errorf("layer read: %v", err)
		}
		rc.Close()
	}
}

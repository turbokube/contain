package ocipush_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/turbokube/contain/pkg/ocipush"
)

// TestProxyPushPull pushes with a stock registry client (go-containerregistry
// remote, which drives the same POST/PATCH/PUT flow as docker) through the
// proxy to an ext-extension upstream, then pulls back via both the proxy and
// the upstream directly.
func TestProxyPushPull(t *testing.T) {
	fake := &extFake{reg: quietRegistry(), staged: map[string]map[int][]byte{}}
	upstream := httptest.NewServer(fake)
	defer upstream.Close()
	fake.serverURL = upstream.URL
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	proxy, err := ocipush.NewProxy(upstreamHost, "", ocipush.Options{
		Auth:         authn.Anonymous,
		ExtThreshold: 1, // everything large enough goes direct
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()
	proxyHost := strings.TrimPrefix(proxyServer.URL, "http://")

	img, err := random.Image(3500, 2) // layers > fakePartSize to exercise multipart
	if err != nil {
		t.Fatal(err)
	}
	want, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	proxyRef, err := name.ParseReference(proxyHost + "/test/viaproxy:v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(proxyRef, img, remote.WithAuth(authn.Anonymous)); err != nil {
		t.Fatalf("push via proxy: %v", err)
	}
	if fake.uploads == 0 || fake.commits == 0 {
		t.Errorf("upstream extension not exercised: %d uploads %d commits", fake.uploads, fake.commits)
	}

	// pull via proxy
	viaProxy, err := remote.Get(proxyRef, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull via proxy: %v", err)
	}
	if viaProxy.Digest.String() != want.String() {
		t.Errorf("digest via proxy: pushed %s got %s", want, viaProxy.Digest)
	}

	// pull directly from upstream: the proxy must not have rewritten anything
	upstreamRef, err := name.ParseReference(upstreamHost + "/test/viaproxy:v1")
	if err != nil {
		t.Fatal(err)
	}
	direct, err := remote.Get(upstreamRef, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull from upstream: %v", err)
	}
	if direct.Digest.String() != want.String() {
		t.Errorf("digest upstream: pushed %s got %s", want, direct.Digest)
	}

	// layer content must round-trip through proxy pull
	pulled, err := remote.Image(proxyRef, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatal(err)
	}
	layers, err := pulled.Layers()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range layers {
		rc, err := l.Compressed()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			t.Errorf("layer read via proxy: %v", err)
		}
		rc.Close()
	}
}

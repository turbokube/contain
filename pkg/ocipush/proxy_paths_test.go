package ocipush_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/turbokube/contain/pkg/ocipush"
)

// TestProxyRejectsPathsOutsideTheRepositoryGrammar makes sure no route
// interpolates client-controlled path segments into an upstream request.
//
// The proxy attaches the upstream's credentials to everything it forwards.
// Path segments arrive unnormalized, so ".." survives strings.FieldsFunc, and
// while Go's transport sends the path as-is, upstreams and CDNs commonly
// resolve dot segments — including Cloudflare, which is the deployment this
// proxy exists for. Before this was fixed, only the manifests and
// blobs/uploads routes validated, so /v2/../../admin/blobs/... and
// /v2/../../admin/tags/list reached the upstream authenticated.
func TestProxyRejectsPathsOutsideTheRepositoryGrammar(t *testing.T) {
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxy, err := ocipush.NewProxy(strings.TrimPrefix(upstream.URL, "http://"), "team/", ocipush.Options{
		Auth: authn.Anonymous,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()

	rejected := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v2/../../admin/blobs/sha256:aa"},
		{http.MethodGet, "/v2/a/../../../admin/blobs/sha256:aa"},
		{http.MethodHead, "/v2/../../admin/blobs/sha256:aa"},
		{http.MethodGet, "/v2/a/../../../admin/tags/list"},
		{http.MethodGet, "/v2/../../admin/manifests/latest"},
		{http.MethodPost, "/v2/../../admin/blobs/uploads"},
		{http.MethodPatch, "/v2/../../admin/blobs/uploads/deadbeef"},
		// upper case is outside the OCI repository grammar
		{http.MethodGet, "/v2/UPPER/blobs/sha256:aa"},
		// dot segments in the trailing element, not just the name
		{http.MethodGet, "/v2/ok/blobs/.."},
		{http.MethodGet, "/v2/ok/manifests/.."},
		// upload ids are ours; anything else cannot name a session
		{http.MethodPatch, "/v2/ok/blobs/uploads/../../admin"},
	}

	for _, c := range rejected {
		forwarded.Store(0)
		req, err := http.NewRequest(c.method, server.URL+c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		// RoundTrip, not Client.Do: net/http's URL handling would otherwise
		// clean the path before it ever reaches the proxy
		res, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		res.Body.Close()
		if res.StatusCode/100 == 2 {
			t.Errorf("%s %s: got %d, want a rejection", c.method, c.path, res.StatusCode)
		}
		if n := forwarded.Load(); n != 0 {
			t.Errorf("%s %s: reached upstream %d times, want 0", c.method, c.path, n)
		}
	}
}

// Legitimate multi-segment repository names must keep working: the OCI name
// grammar allows slashes, which is why the router matches from the end.
func TestProxyAcceptsNestedRepositoryNames(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxy, err := ocipush.NewProxy(strings.TrimPrefix(upstream.URL, "http://"), "team/", ocipush.Options{
		Auth: authn.Anonymous,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()

	for _, c := range []struct{ path, want string }{
		{"/v2/a/b/c/manifests/v1.2.3", "/v2/team/a/b/c/manifests/v1.2.3"},
		{"/v2/a/b/blobs/sha256:" + strings.Repeat("a", 64), "/v2/team/a/b/blobs/sha256:" + strings.Repeat("a", 64)},
		{"/v2/my-app_1/tags/list", "/v2/team/my-app_1/tags/list"},
	} {
		gotPath = ""
		res, err := http.Get(server.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d", c.path, res.StatusCode)
		}
		if gotPath != c.want {
			t.Errorf("GET %s: upstream saw %q, want %q", c.path, gotPath, c.want)
		}
	}
}

// upstreamStatusFake answers the /v2/ ping like a real registry, so the auth
// handshake succeeds, then refuses the actual API call with one chosen
// status. That isolates the proxy's error mapping from the handshake.
type upstreamStatusFake struct{ status int }

func (f upstreamStatusFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v2/" || r.URL.Path == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.status)
	w.Write([]byte(`{"errors":[{"code":"DENIED","message":"upstream said no"}]}`)) //nolint:errcheck
}

// The proxy used to flatten every upstream failure to 502 UNKNOWN, which
// tells a docker client nothing: a credentials problem and a genuinely
// broken gateway looked identical. A status the upstream chose deliberately
// is relayed.
func TestProxyRelaysUpstreamStatus(t *testing.T) {
	for _, c := range []struct {
		upstream int
		want     int
	}{
		{http.StatusUnauthorized, http.StatusUnauthorized},
		{http.StatusForbidden, http.StatusForbidden},
		{http.StatusTooManyRequests, http.StatusTooManyRequests},
		// a genuine gateway problem stays a gateway problem
		{http.StatusInternalServerError, http.StatusBadGateway},
		// 404 on this route is not a failure at all: the blob simply is not
		// there to mount, so the proxy opens an ordinary upload session
		{http.StatusNotFound, http.StatusAccepted},
	} {
		upstream := httptest.NewServer(upstreamStatusFake{status: c.upstream})
		proxy, err := ocipush.NewProxy(strings.TrimPrefix(upstream.URL, "http://"), "",
			ocipush.Options{Auth: authn.Anonymous})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(proxy)

		res, err := http.Post(server.URL+"/v2/team/app/blobs/uploads/?mount=sha256:"+strings.Repeat("a", 64),
			"application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != c.want {
			t.Errorf("upstream %d: proxy answered %d, want %d", c.upstream, res.StatusCode, c.want)
		}
		server.Close()
		upstream.Close()
	}
}

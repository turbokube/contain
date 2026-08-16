package ocipush_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/turbokube/contain/pkg/ocipush"
)

// tokenRegistry wraps a registry with the distribution-spec bearer handshake
// that Docker Hub, GHCR, GCR, ECR and Quay all use: unauthenticated /v2/
// requests get a 401 with a WWW-Authenticate challenge, and only a token
// obtained from the realm with the right scope is accepted. A client that
// merely sets a static Authorization header never gets past this.
type tokenRegistry struct {
	reg    http.Handler
	url    string
	user   string
	pass   string
	issued atomic.Int32
	scopes chan string
}

const testToken = "test-bearer-token"

func (tr *tokenRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/token" {
		u, p, ok := basicFrom(r.Header.Get("Authorization"))
		if !ok || u != tr.user || p != tr.pass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		tr.issued.Add(1)
		select {
		case tr.scopes <- r.URL.Query().Get("scope"):
		default:
		}
		json.NewEncoder(w).Encode(map[string]any{"token": testToken}) //nolint:errcheck
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		w.Header().Set("WWW-Authenticate",
			`Bearer realm="`+tr.url+`/token",service="testregistry"`)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"errors": []map[string]any{{"code": "UNAUTHORIZED", "message": "authentication required"}},
		})
		return
	}
	tr.reg.ServeHTTP(w, r)
}

// localhostURL rewrites an httptest URL from 127.0.0.1 to localhost.
// go-containerregistry refuses a WWW-Authenticate realm whose host is a
// loopback or private IP literal (SSRF protection in validateRealmURL), and
// "localhost" is not an IP literal, so the fixture can advertise its own
// token endpoint. It also makes name.ParseReference treat the registry as
// insecure, which is what allows an http realm.
func localhostURL(serverURL string) string {
	return strings.Replace(serverURL, "127.0.0.1", "localhost", 1)
}

func basicFrom(header string) (string, string, bool) {
	if !strings.HasPrefix(header, "Basic ") {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, "Basic "))
	if err != nil {
		return "", "", false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	return user, pass, ok
}

// TestPushTokenAuthRegistry is the regression test for the auth handshake:
// push must work against a registry that answers 401 with a bearer challenge,
// which is every hosted registry. Before the client used
// go-containerregistry's transport it sent a static Basic header and every
// request failed with 401 UNAUTHORIZED.
func TestPushTokenAuthRegistry(t *testing.T) {
	dir, img := layoutWithImage(t, 1024, 2)

	tr := &tokenRegistry{
		reg:    quietRegistry(),
		user:   "someuser",
		pass:   "somepass",
		scopes: make(chan string, 32),
	}
	server := httptest.NewServer(tr)
	defer server.Close()
	tr.url = localhostURL(server.URL)
	host := strings.TrimPrefix(tr.url, "http://")
	image := host + "/test/tokenauth:v1"

	err := ocipush.Push(context.Background(), dir, image, ocipush.Options{
		Auth: authn.FromConfig(authn.AuthConfig{Username: tr.user, Password: tr.pass}),
	})
	if err != nil {
		t.Fatalf("push to token-auth registry: %v", err)
	}
	if tr.issued.Load() == 0 {
		t.Error("no token was ever requested, the handshake did not happen")
	}

	// tokens must be scoped to the repository being pushed
	close(tr.scopes)
	sawPush := false
	for scope := range tr.scopes {
		if !strings.HasPrefix(scope, "repository:test/tokenauth:") {
			t.Errorf("unexpected token scope %q", scope)
		}
		if strings.Contains(scope, "push") {
			sawPush = true
		}
	}
	if !sawPush {
		t.Error("no push-scoped token requested")
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remoteGetWithToken(ref)
	if err != nil {
		t.Fatalf("pull after push: %v", err)
	}
	want, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if got != want.String() {
		t.Errorf("digest not preserved: pushed %s got %s", want, got)
	}
}

// remoteGetWithToken reads the manifest back using the bearer token directly,
// independent of the client under test.
func remoteGetWithToken(ref name.Reference) (string, error) {
	req, err := http.NewRequest(http.MethodGet,
		"http://"+ref.Context().RegistryStr()+"/v2/"+ref.Context().RepositoryStr()+"/manifests/"+ref.Identifier(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body) //nolint:errcheck
	return res.Header.Get("Docker-Content-Digest"), nil
}

// TestMirrorTokenAuthSource covers the read side: a token-auth source is what
// mirroring from Docker Hub or GHCR looks like.
func TestMirrorTokenAuthSource(t *testing.T) {
	tr := &tokenRegistry{
		reg:    quietRegistry(),
		user:   "u",
		pass:   "p",
		scopes: make(chan string, 32),
	}
	srcServer := httptest.NewServer(tr)
	defer srcServer.Close()
	tr.url = localhostURL(srcServer.URL)
	srcHost := strings.TrimPrefix(tr.url, "http://")

	dstServer := httptest.NewServer(quietRegistry())
	defer dstServer.Close()
	dstHost := strings.TrimPrefix(localhostURL(dstServer.URL), "http://")

	img, err := random.Image(1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	// seed the source through the token handshake
	srcRef, err := name.ParseReference(srcHost + "/team/app:v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWithToken(srcRef, img); err != nil {
		t.Fatal(err)
	}

	err = ocipush.Mirror(context.Background(), srcHost+"/team/app:v1", dstHost+"/team/app:v1",
		ocipush.MirrorOptions{
			Src: ocipush.SourceOptions{Auth: authn.FromConfig(authn.AuthConfig{Username: tr.user, Password: tr.pass})},
			Dst: ocipush.Options{Auth: authn.Anonymous},
		})
	if err != nil {
		t.Fatalf("mirror from token-auth source: %v", err)
	}
	if tr.issued.Load() == 0 {
		t.Error("source token handshake did not happen")
	}
}

// writeWithToken seeds a token-auth registry using go-containerregistry's own
// transport, so the fixture is independent of the code under test.
func writeWithToken(ref name.Reference, img v1.Image) error {
	return remote.Write(ref, img,
		remote.WithAuth(authn.FromConfig(authn.AuthConfig{Username: "u", Password: "p"})))
}

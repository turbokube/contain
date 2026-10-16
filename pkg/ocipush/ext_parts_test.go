package ocipush_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/turbokube/contain/pkg/ocipush"
)

// cloudflare502 is the page a CDN in front of object storage answers with
// when its origin is unreachable.
const cloudflare502 = `<html>
<head><title>502 Bad Gateway</title></head>
<body>
<center><h1>502 Bad Gateway</h1></center>
<hr><center>cloudflare</center>
</body>
</html>`

// partFaultFake fails the first failPuts part PUTs with fault, and counts
// sessions per digest and attempts per presigned URL.
type partFaultFake struct {
	*extFake
	failPuts int
	fault    func(w http.ResponseWriter, r *http.Request)

	mu       sync.Mutex
	puts     int
	failed   int
	sessions map[string]int
	attempts map[string]int
}

func newPartFaultFake(t *testing.T, expiresSeconds int64, failPuts int, fault func(w http.ResponseWriter, r *http.Request)) (*partFaultFake, string) {
	fake := &partFaultFake{
		extFake: &extFake{
			reg:            quietRegistry(),
			staged:         map[string]map[int][]byte{},
			expiresSeconds: expiresSeconds,
		},
		failPuts: failPuts,
		fault:    fault,
		sessions: map[string]int{},
		attempts: map[string]int{},
	}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fake.serverURL = server.URL
	return fake, hostOf(server)
}

func (f *partFaultFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v2/_directpush/v1/uploads":
		b, _ := io.ReadAll(r.Body)
		var start struct {
			Digest string `json:"digest"`
		}
		json.Unmarshal(b, &start) //nolint:errcheck
		f.mu.Lock()
		f.sessions[start.Digest]++
		f.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(b))
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/stage/"):
		f.mu.Lock()
		f.puts++
		f.attempts[r.URL.String()]++
		fail := f.puts <= f.failPuts
		if fail {
			f.failed++
		}
		f.mu.Unlock()
		if fail {
			f.fault(w, r)
			return
		}
	}
	f.extFake.ServeHTTP(w, r)
}

// assertOneSessionPerBlob: part-level failures are retried on the same URLs,
// never by taking a fresh session.
func (f *partFaultFake) assertOneSessionPerBlob(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sessions) == 0 {
		t.Fatal("no sessions were requested")
	}
	for digest, n := range f.sessions {
		if n != 1 {
			t.Errorf("%s: %d sessions, want 1", digest, n)
		}
	}
}

func (f *partFaultFake) maxAttempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	most := 0
	for _, n := range f.attempts {
		most = max(most, n)
	}
	return most
}

func assertNoPresignedQuery(t *testing.T, err error) {
	t.Helper()
	if strings.Contains(err.Error(), fakeSignature) || strings.Contains(err.Error(), "part=") {
		t.Errorf("error leaks the presigned query string: %v", err)
	}
}

func respondStatus(status int, contentType string, body string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) //nolint:errcheck
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(status)
		w.Write([]byte(body)) //nolint:errcheck
	}
}

func TestPushExtRetriesTransientPartFailure(t *testing.T) {
	dir, img := layoutWithImage(t, 3500, 2)
	fake, host := newPartFaultFake(t, 7200, 1, respondStatus(http.StatusBadGateway, "text/html", cloudflare502))
	image := host + "/test/partretry:v1"

	err := ocipush.Push(context.Background(), dir, image, ocipush.Options{Auth: authn.Anonymous, ExtThreshold: 1})
	if err != nil {
		t.Fatalf("push should survive one 502 on a part: %v", err)
	}
	fake.mu.Lock()
	failed := fake.failed
	fake.mu.Unlock()
	if failed == 0 {
		t.Error("no failure was injected, the test proves nothing")
	}
	fake.assertOneSessionPerBlob(t)
	assertPushedDigest(t, image, img)
	assertLayersReadable(t, image)
}

func TestPushExtPartRetriesAreBounded(t *testing.T) {
	dir, _ := layoutWithImage(t, 3500, 1)
	fake, host := newPartFaultFake(t, 7200, 999, respondStatus(http.StatusBadGateway, "text/html", cloudflare502))

	err := ocipush.Push(context.Background(), dir, host+"/test/partdown:v1", ocipush.Options{Auth: authn.Anonymous, ExtThreshold: 1})
	if err == nil {
		t.Fatal("expected the push to fail")
	}
	if n := fake.maxAttempts(); n < 2 || n > 5 {
		t.Errorf("a part URL got %d attempts, want a few and bounded", n)
	}
	fake.assertOneSessionPerBlob(t)
	if !strings.Contains(err.Error(), `status 502: html page "502 Bad Gateway" from cloudflare, after 4 attempts`) {
		t.Errorf("error should name the status and summarize the page, got: %v", err)
	}
	if strings.Contains(err.Error(), "<center>") {
		t.Errorf("error dumps html markup: %v", err)
	}
	assertNoPresignedQuery(t, err)
}

// A 403 seconds into a long validity window is storage refusing the
// registry's signature, not expiry, and a fresh session would be refused too.
func TestPushExtFreshSessionForbiddenFailsFast(t *testing.T) {
	dir, _ := layoutWithImage(t, 3500, 1)
	fake, host := newPartFaultFake(t, 7200, 999, respondStatus(http.StatusForbidden, "application/xml",
		`<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`))

	err := ocipush.Push(context.Background(), dir, host+"/test/refused:v1", ocipush.Options{Auth: authn.Anonymous, ExtThreshold: 1})
	if err == nil {
		t.Fatal("expected the push to fail")
	}
	for _, want := range []string{"status 403: AccessDenied: Access Denied", "into 7200s validity", "refused a fresh presigned url", "storage credentials"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %q, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "expired") {
		t.Errorf("a fresh session's 403 is not expiry: %v", err)
	}
	if n := fake.maxAttempts(); n != 1 {
		t.Errorf("a part URL got %d attempts, a 403 must not be retried", n)
	}
	fake.assertOneSessionPerBlob(t)
	assertNoPresignedQuery(t, err)
}

// net/http errors print the request URL, which for a part is a credential.
func TestPushExtPartTransportErrorKeepsQueryOut(t *testing.T) {
	dir, _ := layoutWithImage(t, 3500, 1)
	fake, host := newPartFaultFake(t, 7200, 999, func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	})

	err := ocipush.Push(context.Background(), dir, host+"/test/dropped:v1", ocipush.Options{Auth: authn.Anonymous, ExtThreshold: 1})
	if err == nil {
		t.Fatal("expected the push to fail")
	}
	if n := fake.maxAttempts(); n < 2 {
		t.Errorf("a dropped connection should be retried, got %d attempts", n)
	}
	if !strings.Contains(err.Error(), "to http://127.0.0.1:") {
		t.Errorf("error should name the storage host, got: %v", err)
	}
	assertNoPresignedQuery(t, err)
}

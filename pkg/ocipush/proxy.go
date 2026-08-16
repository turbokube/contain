package ocipush

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.uber.org/zap"
)

// Proxy is a localhost OCI registry frontend for an upstream registry whose
// push path is size-limited (e.g. behind Cloudflare's proxy body cap). Stock
// docker/buildkit push to the proxy without limits: blob uploads are staged
// to local temp files, digest-verified, then re-uploaded with pushBlobFile —
// large blobs via the upstream's direct-to-storage extension. Pulls and
// manifest reads pass through with upstream credentials attached.
//
// The proxy itself is unauthenticated: bind it to localhost (default) and
// treat local access as trusted, like docker's own localhost registry
// convention.
type Proxy struct {
	c      *regClient
	prefix string

	mu       sync.Mutex
	sessions map[string]*uploadSession
}

type uploadSession struct {
	mu       sync.Mutex
	file     *os.File
	size     int64
	hash     hash.Hash
	lastUsed time.Time
}

const sessionMaxIdle = time.Hour

var proxyNameRe = regexp.MustCompile(
	`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`)

// proxyRefRe accepts an OCI tag or a digest, the two forms a manifest
// reference can take.
var proxyRefRe = regexp.MustCompile(
	`^([a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}|[a-z0-9]+([.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]+)$`)

var proxyUploadIDRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

// repoOr400 joins repository-name segments and validates them.
//
// Every route must go through this. Path segments arrive unnormalized, so a
// client can send ".." as a segment, and the result is interpolated into an
// upstream URL that carries the upstream's credentials. Go's transport sends
// the path as-is, but upstreams and CDNs commonly resolve dot segments —
// Cloudflare, which is the deployment this proxy exists for, is one — so
// /v2/../../admin/... can become an authenticated request to a path the
// client chose.
func repoOr400(w http.ResponseWriter, segments []string) (string, bool) {
	repo := strings.Join(segments, "/")
	if !proxyNameRe.MatchString(repo) {
		proxyError(w, 400, "NAME_INVALID", "invalid repository name")
		return "", false
	}
	return repo, true
}

// pathPartOr400 validates a single trailing path element (a manifest
// reference, a blob digest, an upload id) for the same reason as repoOr400.
func pathPartOr400(w http.ResponseWriter, value string, re *regexp.Regexp, code string, message string) bool {
	if !re.MatchString(value) {
		proxyError(w, 400, code, message)
		return false
	}
	return true
}

// NewProxy creates a proxy for the given upstream registry host. prefix, when
// non-empty, is prepended to every repository name (use for registries that
// namespace teams/projects); it must end with "/".
func NewProxy(upstreamHost string, prefix string, opts Options) (*Proxy, error) {
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		return nil, fmt.Errorf("prefix must end with /: %q", prefix)
	}
	reg, err := name.NewRegistry(upstreamHost)
	if err != nil {
		return nil, fmt.Errorf("upstream registry %s: %w", upstreamHost, err)
	}
	c, err := newRegClient(reg, opts)
	if err != nil {
		return nil, err
	}
	return &Proxy{c: c, prefix: prefix, sessions: map[string]*uploadSession{}}, nil
}

func (p *Proxy) upstreamRepo(repo string) string {
	return p.prefix + repo
}

func proxyError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"errors": []map[string]any{{"code": code, "message": message, "detail": nil}},
	})
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segments := strings.FieldsFunc(r.URL.Path, func(c rune) bool { return c == '/' })
	if len(segments) == 0 || segments[0] != "v2" {
		proxyError(w, 404, "UNSUPPORTED", "not found")
		return
	}
	rest := segments[1:]
	n := len(rest)

	if n == 0 {
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}")) //nolint:errcheck
		return
	}

	// Match from the END of the path; <name> may contain slashes.
	switch {
	case n >= 3 && rest[n-2] == "manifests":
		repo, ok := repoOr400(w, rest[:n-2])
		if !ok {
			return
		}
		ref := rest[n-1]
		if !pathPartOr400(w, ref, proxyRefRe, "MANIFEST_INVALID", "invalid manifest reference") {
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p.forward(w, r, p.upstreamRepo(repo), fmt.Sprintf("/v2/%s/manifests/%s", p.upstreamRepo(repo), ref))
		case http.MethodPut:
			p.manifestPut(w, r, repo, ref)
		default:
			proxyError(w, 405, "UNSUPPORTED", "method not allowed")
		}
	case n >= 3 && rest[n-2] == "blobs" && rest[n-1] == "uploads":
		repo, ok := repoOr400(w, rest[:n-2])
		if !ok {
			return
		}
		if r.Method != http.MethodPost {
			proxyError(w, 405, "UNSUPPORTED", "method not allowed")
			return
		}
		p.uploadStart(w, r, repo)
	case n >= 4 && rest[n-3] == "blobs" && rest[n-2] == "uploads":
		repo, ok := repoOr400(w, rest[:n-3])
		if !ok {
			return
		}
		id := rest[n-1]
		if !pathPartOr400(w, id, proxyUploadIDRe, "BLOB_UPLOAD_INVALID", "invalid upload id") {
			return
		}
		p.uploadContinue(w, r, repo, id)
	case n >= 3 && rest[n-2] == "blobs":
		repo, ok := repoOr400(w, rest[:n-2])
		if !ok {
			return
		}
		digest := rest[n-1]
		if !pathPartOr400(w, digest, proxyRefRe, "DIGEST_INVALID", "invalid digest") {
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			proxyError(w, 405, "UNSUPPORTED", "method not allowed")
			return
		}
		p.forward(w, r, p.upstreamRepo(repo), fmt.Sprintf("/v2/%s/blobs/%s", p.upstreamRepo(repo), digest))
	case n >= 3 && rest[n-2] == "tags" && rest[n-1] == "list":
		repo, ok := repoOr400(w, rest[:n-2])
		if !ok {
			return
		}
		p.forward(w, r, p.upstreamRepo(repo), fmt.Sprintf("/v2/%s/tags/list", p.upstreamRepo(repo)))
	default:
		proxyError(w, 404, "UNSUPPORTED", "not found")
	}
}

// forward relays a read request upstream with credentials. The http client
// follows blob redirects to presigned storage URLs (Go strips the
// Authorization header on cross-host redirects, keeping signatures valid).
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, upstreamRepo string, upstreamPath string) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, p.c.base+upstreamPath, nil)
	if err != nil {
		proxyError(w, 500, "UNKNOWN", err.Error())
		return
	}
	for _, h := range []string{"Accept", "Range"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	res, err := p.c.do(upstreamRepo, transport.PullScope, req)
	if err != nil {
		proxyError(w, 502, "UNKNOWN", fmt.Sprintf("upstream: %v", err))
		return
	}
	defer res.Body.Close()
	for _, h := range []string{
		"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges",
		"Docker-Content-Digest", "Etag", "Link",
	} {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if _, err := io.Copy(w, res.Body); err != nil {
		zap.L().Debug("forward copy interrupted", zap.Error(err))
	}
}

func (p *Proxy) manifestPut(w http.ResponseWriter, r *http.Request, repo string, ref string) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32*1024*1024+1))
	if err != nil {
		proxyError(w, 400, "MANIFEST_INVALID", err.Error())
		return
	}
	if len(raw) > 32*1024*1024 {
		proxyError(w, 413, "MANIFEST_INVALID", "manifest too large")
		return
	}
	mediaType := r.Header.Get("Content-Type")
	if err := p.c.putManifest(r.Context(), p.upstreamRepo(repo), raw, mediaType, ref); err != nil {
		proxyError(w, 502, "UNKNOWN", err.Error())
		return
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/manifests/%s", repo, digest))
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

func (p *Proxy) uploadStart(w http.ResponseWriter, r *http.Request, repo string) {
	// Cross-repo mount: satisfied iff the blob already exists upstream.
	if mount := r.URL.Query().Get("mount"); mount != "" {
		exists, err := p.c.blobExists(r.Context(), p.upstreamRepo(repo), mount)
		if err != nil {
			proxyError(w, 502, "UNKNOWN", err.Error())
			return
		}
		if exists {
			w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo, mount))
			w.Header().Set("Docker-Content-Digest", mount)
			w.WriteHeader(http.StatusCreated)
			return
		}
	}

	file, err := os.CreateTemp("", "contain-proxy-upload-*")
	if err != nil {
		proxyError(w, 500, "UNKNOWN", err.Error())
		return
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		proxyError(w, 500, "UNKNOWN", err.Error())
		return
	}
	id := hex.EncodeToString(idBytes)
	session := &uploadSession{file: file, hash: sha256.New(), lastUsed: time.Now()}
	p.mu.Lock()
	p.expireIdleSessionsLocked()
	p.sessions[id] = session
	p.mu.Unlock()

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, id))
	w.Header().Set("Docker-Upload-UUID", id)
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

func (p *Proxy) expireIdleSessionsLocked() {
	for id, s := range p.sessions {
		if time.Since(s.lastUsed) > sessionMaxIdle && s.mu.TryLock() {
			s.file.Close()           //nolint:errcheck
			os.Remove(s.file.Name()) //nolint:errcheck
			delete(p.sessions, id)
			s.mu.Unlock()
		}
	}
}

func (p *Proxy) uploadContinue(w http.ResponseWriter, r *http.Request, repo string, id string) {
	p.mu.Lock()
	session := p.sessions[id]
	p.mu.Unlock()
	if session == nil {
		proxyError(w, 404, "BLOB_UPLOAD_UNKNOWN", "unknown upload session")
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.lastUsed = time.Now()

	drop := func() {
		session.file.Close()           //nolint:errcheck
		os.Remove(session.file.Name()) //nolint:errcheck
		p.mu.Lock()
		delete(p.sessions, id)
		p.mu.Unlock()
	}

	switch r.Method {
	case http.MethodPatch:
		if cr := r.Header.Get("Content-Range"); cr != "" && !strings.HasPrefix(cr, fmt.Sprintf("%d-", session.size)) {
			proxyError(w, 416, "BLOB_UPLOAD_INVALID", "out of order chunk")
			return
		}
		written, err := io.Copy(io.MultiWriter(session.file, session.hash), r.Body)
		if err != nil {
			drop()
			proxyError(w, 400, "BLOB_UPLOAD_INVALID", err.Error())
			return
		}
		session.size += written
		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, id))
		w.Header().Set("Docker-Upload-UUID", id)
		w.Header().Set("Range", fmt.Sprintf("0-%d", session.size-1))
		w.WriteHeader(http.StatusAccepted)
	case http.MethodPut:
		digest := r.URL.Query().Get("digest")
		if written, err := io.Copy(io.MultiWriter(session.file, session.hash), r.Body); err != nil {
			drop()
			proxyError(w, 400, "BLOB_UPLOAD_INVALID", err.Error())
			return
		} else {
			session.size += written
		}
		actual := fmt.Sprintf("sha256:%x", session.hash.Sum(nil))
		if digest != actual {
			drop()
			proxyError(w, 400, "DIGEST_INVALID", fmt.Sprintf("received %s, client declared %s", actual, digest))
			return
		}
		if err := session.file.Close(); err != nil {
			drop()
			proxyError(w, 500, "UNKNOWN", err.Error())
			return
		}
		err := p.c.pushBlobFile(r.Context(), p.upstreamRepo(repo),
			descriptor{Digest: digest, Size: session.size}, session.file.Name())
		drop()
		if err != nil {
			proxyError(w, 502, "UNKNOWN", fmt.Sprintf("upstream: %v", err))
			return
		}
		w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo, digest))
		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		w.Header().Set("Docker-Upload-UUID", id)
		w.Header().Set("Range", fmt.Sprintf("0-%d", max(session.size-1, 0)))
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		drop()
		w.WriteHeader(http.StatusNoContent)
	default:
		proxyError(w, 405, "UNSUPPORTED", "method not allowed")
	}
}

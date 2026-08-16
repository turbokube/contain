package ocipush

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	partUploadConcurrency = 4
	// extSessionRetries is how many fresh sessions to take after presigned
	// URLs expire mid-upload. Sessions are not resumable, so each retry
	// re-uploads every part.
	extSessionRetries = 1
)

var (
	errExtUnsupported = errors.New("registry does not support the direct-upload extension")
	errExtExpired     = errors.New("presigned upload urls expired")
)

// regClient talks to one upstream registry; repository is per call so the
// registry-proxy can serve many repos through one client.
//
// Requests go through go-containerregistry's transport, which performs the
// distribution-spec auth handshake: ping, read the WWW-Authenticate challenge,
// and exchange credentials for a scoped bearer token where the registry asks
// for one. Every hosted registry (Docker Hub, GHCR, GCR, ECR, Quay) requires
// that; a static Authorization header only works against registries that take
// Basic auth directly. Tokens are scoped per repository and action, so
// authenticated clients are built lazily and cached per scope.
type regClient struct {
	base string // scheme://host of the registry
	reg  name.Registry
	auth authn.Authenticator
	// inner is the unauthenticated base transport. Presigned storage URLs must
	// use it directly: their signature covers no Authorization header.
	inner http.RoundTripper
	raw   *http.Client

	mu      sync.Mutex
	clients map[string]*http.Client

	discoverOnce sync.Once
	extSupported bool
	extDisabled  atomic.Bool
	extThreshold int64
	partSize     int64
}

type extDiscoverResponse struct {
	Extensions []struct {
		Name      string   `json:"name"`
		Endpoints []string `json:"endpoints"`
	} `json:"extensions"`
}

// directpushSupported detects the _directpush extension via the OCI
// extensions discovery endpoint, once per registry. Registries that do not
// positively advertise it get standard OCI uploads exclusively — no probing
// of extension paths (PROTOCOL.md, attached).
//
// repo only selects the token scope. Registries SHOULD serve discovery
// unauthenticated and this one does, but clients must tolerate a challenge,
// and the credentials to answer it with are the ones the extension endpoints
// use: repository:<repo>:push,pull. Sending that token unconditionally covers
// both cases in one round trip, and it is a client we need for the push
// anyway.
func (c *regClient) directpushSupported(ctx context.Context, repo string) bool {
	c.discoverOnce.Do(func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v2/_oci/ext/discover", nil)
		if err != nil {
			return
		}
		res, err := c.do(repo, transport.PushScope, req)
		if err != nil {
			zap.L().Debug("extension discovery failed", zap.Error(err))
			return
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return
		}
		var doc extDiscoverResponse
		if err := json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&doc); err != nil {
			return
		}
		for _, ext := range doc.Extensions {
			if ext.Name == "_directpush" && slices.Contains(ext.Endpoints, "_directpush/v1/uploads") {
				c.extSupported = true
				zap.L().Debug("registry supports _directpush/v1")
				return
			}
		}
	})
	return c.extSupported
}

func newRegClient(reg name.Registry, opts Options) (*regClient, error) {
	auth := opts.Auth
	if auth == nil {
		keychain := opts.Keychain
		if keychain == nil {
			keychain = authn.DefaultKeychain
		}
		var err error
		auth, err = keychain.Resolve(reg)
		if err != nil {
			return nil, fmt.Errorf("resolve credentials for %s: %w", reg.RegistryStr(), err)
		}
	}

	threshold := opts.ExtThreshold
	if threshold == 0 {
		threshold = DefaultExtThreshold
	}
	inner := opts.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &regClient{
		base:         fmt.Sprintf("%s://%s", reg.Scheme(), reg.RegistryStr()),
		reg:          reg,
		auth:         auth,
		inner:        inner,
		raw:          &http.Client{Transport: inner},
		clients:      map[string]*http.Client{},
		extThreshold: threshold,
		partSize:     opts.PartSize,
	}, nil
}

// client returns an http.Client authenticated for one repository and action,
// performing the auth handshake on first use of each scope and caching the
// result. action is transport.PullScope or transport.PushScope.
func (c *regClient) client(ctx context.Context, repo string, action string) (*http.Client, error) {
	scope := c.reg.Repo(repo).Scope(action)
	c.mu.Lock()
	cached, ok := c.clients[scope]
	c.mu.Unlock()
	if ok {
		return cached, nil
	}
	tr, err := transport.NewWithContext(ctx, c.reg, c.auth, c.inner, []string{scope})
	if err != nil {
		return nil, fmt.Errorf("registry auth for %s: %w", scope, err)
	}
	built := &http.Client{Transport: tr}
	c.mu.Lock()
	defer c.mu.Unlock()
	// another goroutine may have won the race; one client per scope
	if existing, ok := c.clients[scope]; ok {
		return existing, nil
	}
	c.clients[scope] = built
	return built, nil
}

// do sends a request to the registry authenticated for repo and action.
// Presigned storage URLs must NOT go through this: use c.raw, so no
// Authorization header invalidates the signature.
func (c *regClient) do(repo string, action string, req *http.Request) (*http.Response, error) {
	cl, err := c.client(req.Context(), repo, action)
	if err != nil {
		return nil, err
	}
	return cl.Do(req)
}

func errorBody(res *http.Response) string {
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	return string(b)
}

func (c *regClient) blobExists(ctx context.Context, repo string, digest string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		fmt.Sprintf("%s/v2/%s/blobs/%s", c.base, repo, digest), nil)
	if err != nil {
		return false, err
	}
	res, err := c.do(repo, transport.PullScope, req)
	if err != nil {
		return false, fmt.Errorf("blob head %s: %w", digest, err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("blob head %s: unexpected status %d", digest, res.StatusCode)
	}
}

// pushBlobFile uploads the blob in the file at path unless it already exists
// upstream, preferring the direct-to-storage extension for large blobs.
func (c *regClient) pushBlobFile(ctx context.Context, repo string, d descriptor, path string) error {
	exists, err := c.blobExists(ctx, repo, d.Digest)
	if err != nil {
		return err
	}
	if exists {
		zap.L().Debug("blob exists", zap.String("digest", d.Digest), zap.Int64("size", d.Size))
		return nil
	}
	if !c.extDisabled.Load() && d.Size >= c.extThreshold && c.directpushSupported(ctx, repo) {
		err := c.pushBlobExt(ctx, repo, d, path)
		if err == nil {
			return nil
		}
		if err != errExtUnsupported {
			return err
		}
		// safety net: discovery said yes but the endpoint doesn't honor the contract
		c.extDisabled.Store(true)
		zap.L().Warn("registry advertised _directpush but does not honor it, using standard uploads")
	}
	return c.pushBlobStandard(ctx, repo, d, path)
}

// pushBlobStandard is the OCI distribution-spec monolithic upload:
// POST to open a session, PUT the whole blob to the returned location.
func (c *regClient) pushBlobStandard(ctx context.Context, repo string, d descriptor, path string) error {
	postURL := fmt.Sprintf("%s/v2/%s/blobs/uploads/", c.base, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		return err
	}
	res, err := c.do(repo, transport.PushScope, req)
	if err != nil {
		return fmt.Errorf("blob upload start: %w", err)
	}
	if res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("blob upload start: status %d: %s", res.StatusCode, errorBody(res))
	}
	location := res.Header.Get("Location")
	res.Body.Close()
	target, err := resolveLocation(postURL, location, d.Digest)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, target, f)
	if err != nil {
		return err
	}
	put.ContentLength = d.Size
	put.Header.Set("Content-Type", "application/octet-stream")
	putRes, err := c.do(repo, transport.PushScope, put)
	if err != nil {
		return fmt.Errorf("blob put %s: %w", d.Digest, err)
	}
	if putRes.StatusCode != http.StatusCreated {
		return fmt.Errorf("blob put %s: status %d: %s", d.Digest, putRes.StatusCode, errorBody(putRes))
	}
	putRes.Body.Close()
	zap.L().Info("blob pushed", zap.String("digest", d.Digest), zap.Int64("size", d.Size))
	return nil
}

func resolveLocation(requestURL string, location string, digest string) (string, error) {
	if location == "" {
		return "", fmt.Errorf("no Location in upload start response")
	}
	base, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	loc, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("parse upload location: %w", err)
	}
	target := base.ResolveReference(loc)
	q := target.Query()
	q.Set("digest", digest)
	target.RawQuery = q.Encode()
	return target.String(), nil
}

type extStartResponse struct {
	Exists   bool     `json:"exists"`
	Key      string   `json:"key"`
	UploadID string   `json:"uploadId"`
	PartSize int64    `json:"partSize"`
	Urls     []string `json:"urls"`
	// ExpiresSeconds is the presigned URL validity, measured from session
	// creation. Zero or absent means the registry states no deadline.
	ExpiresSeconds int64 `json:"expiresSeconds"`
	// Headers must be sent on every part PUT; the registry uses this to pin
	// e.g. x-amz-checksum-sha256 so object storage verifies content itself.
	Headers map[string]string `json:"headers"`

	// deadline is ExpiresSeconds resolved against the local clock, taken
	// before the request so it never overestimates the remaining validity.
	deadline time.Time
}

type extPart struct {
	PartNumber int    `json:"partNumber"`
	Etag       string `json:"etag"`
}

// pushBlobExt uploads via the registry's direct-to-storage extension:
// presigned part URLs, parallel PUTs to object storage, then a commit call
// that has the registry verify the digest server-side.
//
// Presigned URLs expire. Sessions are not resumable across that, so on expiry
// the whole session is re-requested and every part re-uploaded (PROTOCOL.md,
// "Upload session"). One retry: a second expiry means the blob cannot be
// moved inside the registry's window and a bigger part count or a different
// path is the answer, not more retries.
func (c *regClient) pushBlobExt(ctx context.Context, repo string, d descriptor, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for attempt := 0; ; attempt++ {
		session, err := c.extStartSession(ctx, repo, d)
		if err != nil {
			return err
		}
		if session.Exists {
			return nil
		}
		parts, err := c.extUploadParts(ctx, session, d, f)
		if err != nil {
			if errors.Is(err, errExtExpired) && attempt < extSessionRetries {
				zap.L().Info("direct upload session expired, requesting a fresh one",
					zap.String("digest", d.Digest),
					zap.Int64("expiresSeconds", session.ExpiresSeconds))
				continue
			}
			return fmt.Errorf("direct upload %s: %w", d.Digest, err)
		}
		return c.extCommit(ctx, repo, d, session, parts)
	}
}

// extStartSession opens an upload session, or reports errExtUnsupported when
// the response does not meet the extension contract.
func (c *regClient) extStartSession(ctx context.Context, repo string, d descriptor) (*extStartResponse, error) {
	start := map[string]any{"repository": repo, "digest": d.Digest, "size": d.Size}
	if c.partSize > 0 {
		start["partSize"] = c.partSize
	}
	requested := time.Now()
	res, err := c.postJSON(ctx, repo, "/v2/_directpush/v1/uploads", start)
	if err != nil {
		return nil, fmt.Errorf("direct upload start: %w", err)
	}
	session := &extStartResponse{}
	// The extension contract is 200 + a JSON session. Anything else that isn't
	// a hard error means the registry doesn't implement the extension — some
	// registries route unknown paths loosely (e.g. answer 202 to any
	// */blobs/uploads), so detection must be contract-based, not just 404.
	switch {
	case res.StatusCode == http.StatusOK:
		err := json.NewDecoder(res.Body).Decode(session)
		res.Body.Close()
		if err != nil || (!session.Exists && (len(session.Urls) == 0 || session.PartSize <= 0)) {
			return nil, errExtUnsupported
		}
	case res.StatusCode == http.StatusNotImplemented:
		// Advertised but not operational. The spec uses 501 UNSUPPORTED for a
		// blob outside what the registry's current mode can take, and a
		// registry whose discovery document does not track its own runtime
		// capability will answer it for every session. Standard upload is a
		// better answer than failing the push either way, but the reason
		// would otherwise be lost, so say it once.
		zap.L().Warn("registry declined the direct upload session, falling back to standard upload",
			zap.String("digest", d.Digest), zap.Int64("size", d.Size),
			zap.String("response", errorBody(res)))
		return nil, errExtUnsupported
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden ||
		res.StatusCode == http.StatusBadRequest || res.StatusCode >= 500:
		return nil, fmt.Errorf("direct upload start: status %d: %s", res.StatusCode, errorBody(res))
	default:
		res.Body.Close()
		return nil, errExtUnsupported
	}
	if session.ExpiresSeconds > 0 {
		session.deadline = requested.Add(time.Duration(session.ExpiresSeconds) * time.Second)
	}
	return session, nil
}

func (c *regClient) extUploadParts(ctx context.Context, session *extStartResponse, d descriptor, f *os.File) ([]extPart, error) {
	parts := make([]extPart, len(session.Urls))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(partUploadConcurrency)
	for i, partURL := range session.Urls {
		g.Go(func() error {
			offset := int64(i) * session.PartSize
			length := min(session.PartSize, d.Size-offset)
			if length <= 0 {
				return fmt.Errorf("part %d: no data at offset %d", i+1, offset)
			}
			// A part must begin before the URLs expire; one already in flight
			// may finish after. Checking up front skips a transfer storage is
			// certain to reject.
			if !session.deadline.IsZero() && time.Now().After(session.deadline) {
				return fmt.Errorf("part %d: %w", i+1, errExtExpired)
			}
			req, err := http.NewRequestWithContext(gctx, http.MethodPut, partURL,
				io.NewSectionReader(f, offset, length))
			if err != nil {
				return err
			}
			req.ContentLength = length
			// Exactly the prescribed headers and nothing else. A presigned
			// signature covers a fixed header set, so an extra one (any
			// x-amz-*, or Content-Type on some backends) invalidates it.
			for k, v := range session.Headers {
				req.Header.Set(k, v)
			}
			// presigned URL: no registry credentials, raw transport
			res, err := c.raw.Do(req)
			if err != nil {
				return fmt.Errorf("part %d: %w", i+1, err)
			}
			if res.StatusCode == http.StatusForbidden {
				// how object storage reports a presigned URL past its lifetime
				return fmt.Errorf("part %d: %s: %w", i+1, errorBody(res), errExtExpired)
			}
			if res.StatusCode < 200 || res.StatusCode > 299 {
				return fmt.Errorf("part %d: status %d: %s", i+1, res.StatusCode, errorBody(res))
			}
			parts[i] = extPart{PartNumber: i + 1, Etag: res.Header.Get("ETag")}
			res.Body.Close()
			zap.L().Debug("part uploaded", zap.String("digest", d.Digest),
				zap.Int("part", i+1), zap.Int("of", len(session.Urls)))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return parts, nil
}

func (c *regClient) extCommit(ctx context.Context, repo string, d descriptor, session *extStartResponse, parts []extPart) error {
	commit := map[string]any{"repository": repo, "digest": d.Digest, "size": d.Size, "key": session.Key}
	if session.UploadID != "" {
		commit["uploadId"] = session.UploadID
		commit["parts"] = parts
	}
	commitRes, err := c.postJSON(ctx, repo, "/v2/_directpush/v1/commit", commit)
	if err != nil {
		return fmt.Errorf("direct upload commit: %w", err)
	}
	if commitRes.StatusCode != http.StatusOK {
		return fmt.Errorf("direct upload commit %s: status %d: %s", d.Digest, commitRes.StatusCode, errorBody(commitRes))
	}
	commitRes.Body.Close()
	zap.L().Info("blob pushed direct", zap.String("digest", d.Digest),
		zap.Int64("size", d.Size), zap.Int("parts", len(session.Urls)))
	return nil
}

func (c *regClient) postJSON(ctx context.Context, repo string, path string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(repo, transport.PushScope, req)
}

func (c *regClient) putManifest(ctx context.Context, repo string, raw []byte, mediaType string, refOrDigest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		fmt.Sprintf("%s/v2/%s/manifests/%s", c.base, repo, refOrDigest), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mediaType)
	res, err := c.do(repo, transport.PushScope, req)
	if err != nil {
		return fmt.Errorf("manifest put %s: %w", refOrDigest, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("manifest put %s: status %d: %s", refOrDigest, res.StatusCode, errorBody(res))
	}
	res.Body.Close()
	zap.L().Info("manifest pushed", zap.String("ref", refOrDigest), zap.String("mediaType", mediaType))
	return nil
}

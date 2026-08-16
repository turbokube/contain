package ocipush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.uber.org/zap"
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
	// raw is the unauthenticated client. Presigned storage URLs must use it:
	// their signature covers no Authorization header. Its transport is also
	// the base the scoped clients below are built on, so connections are
	// pooled across both.
	raw *http.Client

	mu      sync.Mutex
	clients map[string]*http.Client

	// ext is the optional direct-to-storage extension, nil when this client
	// speaks standard OCI only. See directpush.go.
	ext *directPush
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
		base:    fmt.Sprintf("%s://%s", reg.Scheme(), reg.RegistryStr()),
		reg:     reg,
		auth:    auth,
		raw:     &http.Client{Transport: inner},
		clients: map[string]*http.Client{},
		ext:     &directPush{threshold: threshold, partSize: opts.PartSize},
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
	tr, err := transport.NewWithContext(ctx, c.reg, c.auth, c.raw.Transport, []string{scope})
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

// blobExists reports whether the registry already has the blob. action is the
// scope to ask with: push flows pass PushScope so they reuse the token they
// need anyway rather than making the registry mint a second, pull-only one
// (transport.PushScope is "push,pull", so it subsumes this read).
func (c *regClient) blobExists(ctx context.Context, repo string, digest string, action string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		fmt.Sprintf("%s/v2/%s/blobs/%s", c.base, repo, digest), nil)
	if err != nil {
		return false, err
	}
	res, err := c.do(repo, action, req)
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
	exists, err := c.blobExists(ctx, repo, d.Digest, transport.PushScope)
	if err != nil {
		return err
	}
	if exists {
		zap.L().Debug("blob exists", zap.String("digest", d.Digest), zap.Int64("size", d.Size))
		return nil
	}
	handled, err := c.tryDirectPush(ctx, repo, d, path)
	if err != nil {
		return err
	}
	if handled {
		return nil
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

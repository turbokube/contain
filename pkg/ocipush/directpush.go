package ocipush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// _directpush is an optional registry extension: the registry hands out
// presigned object-storage URLs and blob bytes go straight there, bypassing
// any body-size limit on the path to registry compute. Spec: PROTOCOL.md
// (attached).
//
// Everything the extension needs lives in directPush so that regClient, and
// the standard OCI upload path in client.go, stay unaware of it. A nil
// *directPush is a client that only ever speaks standard OCI, which is what
// every registry that does not advertise the extension gets.

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

// directPush holds the extension's state for one registry.
type directPush struct {
	// threshold is the blob size at or above which the extension is preferred.
	threshold int64
	// partSize is proposed to the registry; the registry decides.
	partSize int64

	// usable is whether this registry advertises the extension and honours it.
	// discoverOnce keeps discovery to one request per registry, as the spec
	// asks (result cached per registry per process).
	discoverOnce sync.Once
	usable       atomic.Bool
}

// wants reports whether this blob should be attempted via the extension.
// A nil receiver means the extension is disabled entirely.
func (e *directPush) wants(size int64) bool {
	return e != nil && size >= e.threshold
}

// tryDirectPush uploads the blob through the extension if this registry
// advertises it and the blob is big enough to be worth the extra round
// trips. It reports whether it handled the blob; false means the caller
// should use the standard OCI upload, and is not an error.
//
// The extension is optional at every step: too small, not advertised, or
// advertised but not honoured all land on the standard path. The last of
// those also latches the extension off for the rest of the process, so one
// misbehaving registry costs one session request rather than one per blob.
func (c *regClient) tryDirectPush(ctx context.Context, repo string, d descriptor, path string) (bool, error) {
	if !c.ext.wants(d.Size) || !c.directpushSupported(ctx, repo) {
		return false, nil
	}
	err := c.pushBlobExt(ctx, repo, d, path)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, errExtUnsupported) {
		return false, err
	}
	c.ext.usable.Store(false)
	zap.L().Warn("registry advertised _directpush but does not honor it, using standard uploads")
	return false, nil
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
	c.ext.discoverOnce.Do(func() {
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
				c.ext.usable.Store(true)
				zap.L().Debug("registry supports _directpush/v1")
				return
			}
		}
	})
	return c.ext.usable.Load()
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
	if c.ext.partSize > 0 {
		start["partSize"] = c.ext.partSize
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
			// drain before close, or a backend that answers with a body (some
			// S3-compatible gateways return XML) costs a fresh connection per part
			io.Copy(io.Discard, res.Body) //nolint:errcheck
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

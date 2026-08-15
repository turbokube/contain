package ocipush

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const partUploadConcurrency = 4

var errExtUnsupported = errors.New("registry does not support the direct-upload extension")

type pusher struct {
	layout       string
	repo         string // repository path within the registry
	base         string // scheme://host of the registry
	authHeader   string
	client       *http.Client
	ext          bool
	extThreshold int64
	partSize     int64
}

func newPusher(ref name.Reference, layoutDir string, opts Options) (*pusher, error) {
	reg := ref.Context().Registry
	auth := opts.Auth
	if auth == nil {
		keychain := opts.Keychain
		if keychain == nil {
			keychain = authn.DefaultKeychain
		}
		var err error
		auth, err = keychain.Resolve(ref.Context())
		if err != nil {
			return nil, fmt.Errorf("resolve credentials for %s: %w", reg.RegistryStr(), err)
		}
	}
	authConfig, err := auth.Authorization()
	if err != nil {
		return nil, fmt.Errorf("authorization: %w", err)
	}
	header := ""
	switch {
	case authConfig.Username != "" || authConfig.Password != "":
		header = "Basic " + base64.StdEncoding.EncodeToString(
			[]byte(authConfig.Username+":"+authConfig.Password))
	case authConfig.Auth != "":
		header = "Basic " + authConfig.Auth
	case authConfig.RegistryToken != "":
		header = "Bearer " + authConfig.RegistryToken
	}

	threshold := opts.ExtThreshold
	if threshold == 0 {
		threshold = DefaultExtThreshold
	}
	client := http.DefaultClient
	if opts.Transport != nil {
		client = &http.Client{Transport: opts.Transport}
	}
	return &pusher{
		layout:       layoutDir,
		repo:         ref.Context().RepositoryStr(),
		base:         fmt.Sprintf("%s://%s", reg.Scheme(), reg.RegistryStr()),
		authHeader:   header,
		client:       client,
		ext:          true,
		extThreshold: threshold,
		partSize:     opts.PartSize,
	}, nil
}

// do sends a request to the registry with credentials attached. Presigned
// storage URLs must NOT go through this (their signature covers no auth).
func (p *pusher) do(req *http.Request) (*http.Response, error) {
	if p.authHeader != "" {
		req.Header.Set("Authorization", p.authHeader)
	}
	return p.client.Do(req)
}

func errorBody(res *http.Response) string {
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	return string(b)
}

func (p *pusher) blobExists(ctx context.Context, digest string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		fmt.Sprintf("%s/v2/%s/blobs/%s", p.base, p.repo, digest), nil)
	if err != nil {
		return false, err
	}
	res, err := p.do(req)
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

// pushBlobStandard is the OCI distribution-spec monolithic upload:
// POST to open a session, PUT the whole blob to the returned location.
func (p *pusher) pushBlobStandard(ctx context.Context, d descriptor) error {
	postURL := fmt.Sprintf("%s/v2/%s/blobs/uploads/", p.base, p.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		return err
	}
	res, err := p.do(req)
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

	path, err := p.blobPath(d.Digest)
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
	putRes, err := p.do(put)
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
}

type extPart struct {
	PartNumber int    `json:"partNumber"`
	Etag       string `json:"etag"`
}

// pushBlobExt uploads via the registry's direct-to-storage extension:
// presigned part URLs, parallel PUTs to object storage, then a commit call
// that has the registry verify the digest server-side.
func (p *pusher) pushBlobExt(ctx context.Context, d descriptor) error {
	start := map[string]any{"digest": d.Digest, "size": d.Size}
	if p.partSize > 0 {
		start["partSize"] = p.partSize
	}
	session := extStartResponse{}
	res, err := p.postJSON(ctx, "/ext/v1/blobs/uploads", start)
	if err != nil {
		return fmt.Errorf("direct upload start: %w", err)
	}
	// The extension contract is 200 + a JSON session. Anything else that isn't
	// a hard error means the registry doesn't implement the extension — some
	// registries route unknown paths loosely (e.g. answer 202 to any
	// */blobs/uploads), so detection must be contract-based, not just 404.
	switch {
	case res.StatusCode == http.StatusOK:
		err := json.NewDecoder(res.Body).Decode(&session)
		res.Body.Close()
		if err != nil || (!session.Exists && (len(session.Urls) == 0 || session.PartSize <= 0)) {
			return errExtUnsupported
		}
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden ||
		res.StatusCode == http.StatusBadRequest || res.StatusCode >= 500:
		return fmt.Errorf("direct upload start: status %d: %s", res.StatusCode, errorBody(res))
	default:
		res.Body.Close()
		return errExtUnsupported
	}
	if session.Exists {
		return nil
	}

	path, err := p.blobPath(d.Digest)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

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
			req, err := http.NewRequestWithContext(gctx, http.MethodPut, partURL,
				io.NewSectionReader(f, offset, length))
			if err != nil {
				return err
			}
			req.ContentLength = length
			// presigned URL: no registry credentials, p.client directly
			res, err := p.client.Do(req)
			if err != nil {
				return fmt.Errorf("part %d: %w", i+1, err)
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
		return fmt.Errorf("direct upload %s: %w", d.Digest, err)
	}

	commit := map[string]any{"digest": d.Digest, "size": d.Size, "key": session.Key}
	if session.UploadID != "" {
		commit["uploadId"] = session.UploadID
		commit["parts"] = parts
	}
	commitRes, err := p.postJSON(ctx, "/ext/v1/blobs/commit", commit)
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

func (p *pusher) postJSON(ctx context.Context, path string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return p.do(req)
}

func (p *pusher) putManifest(ctx context.Context, raw []byte, mediaType string, refOrDigest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		fmt.Sprintf("%s/v2/%s/manifests/%s", p.base, p.repo, refOrDigest), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mediaType)
	res, err := p.do(req)
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

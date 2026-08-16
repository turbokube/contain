package ocipush

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.uber.org/zap"
)

const manifestAccept = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

// SourceOptions is how to reach a registry that is only read from. It is
// deliberately not Options: a source client issues nothing but GETs, so the
// push-side settings (ExtThreshold, PartSize, StagingDir) have no meaning
// there and are structurally absent rather than silently ignored.
type SourceOptions struct {
	// Auth overrides keychain resolution (e.g. authn.Anonymous in tests).
	Auth authn.Authenticator
	// Keychain resolves registry credentials; defaults to authn.DefaultKeychain.
	Keychain authn.Keychain
	// Transport overrides the default http transport.
	Transport http.RoundTripper
	// PlainHTTP uses http:// for this registry (e.g. cluster-internal
	// registries). Content integrity still holds: every manifest and blob is
	// verified against its digest before being pushed.
	PlainHTTP bool
}

// access is how the source is reached, as the registry client wants it.
func (s SourceOptions) access() Options {
	return Options{Auth: s.Auth, Keychain: s.Keychain, Transport: s.Transport}
}

type MirrorOptions struct {
	// Src is read-only access to the registry being copied from.
	Src SourceOptions
	// Dst is the destination, including how to push to it.
	Dst Options
}

// Mirror copies the image at srcRef (tag, digest, or tag@digest) to dstRef,
// like crane cp but with the direct-to-storage extension on the push side.
// All manifests and blobs transfer as raw bytes; every node in the tree is
// digest-verified before it is pushed, so a compromised or plain-http source
// cannot inject content under a wrong digest.
func Mirror(ctx context.Context, srcRef string, dstRef string, opts MirrorOptions) error {
	var srcNameOpts []name.Option
	if opts.Src.PlainHTTP {
		srcNameOpts = append(srcNameOpts, name.Insecure)
	}
	src, err := name.ParseReference(srcRef, srcNameOpts...)
	if err != nil {
		return fmt.Errorf("parse source %s: %w", srcRef, err)
	}
	dst, err := name.ParseReference(dstRef)
	if err != nil {
		return fmt.Errorf("parse destination %s: %w", dstRef, err)
	}
	srcClient, err := newRegClient(src.Context().Registry, opts.Src.access())
	if err != nil {
		return err
	}
	dstClient, err := newRegClient(dst.Context().Registry, opts.Dst)
	if err != nil {
		return err
	}
	m := &mirrorer{
		src: srcClient, dst: dstClient,
		srcRepo: src.Context().RepositoryStr(), dstRepo: dst.Context().RepositoryStr(),
		stagingDir: opts.Dst.StagingDir,
	}
	logStagingDir(m.stagingDir)

	raw, mediaType, err := m.fetchManifest(ctx, src.Identifier())
	if err != nil {
		return err
	}
	// A digest source reference pins the root; verify before trusting the tree.
	if d, ok := src.(name.Digest); ok {
		actual := digestOf(raw)
		if actual != d.DigestStr() {
			return fmt.Errorf("source root manifest digest %s does not match requested %s", actual, d.DigestStr())
		}
	}
	zap.L().Info("mirroring", zap.String("src", src.String()), zap.String("dst", dst.String()))
	return m.pushTree(ctx, raw, mediaType, dst.Identifier())
}

type mirrorer struct {
	src        *regClient
	dst        *regClient
	srcRepo    string
	dstRepo    string
	stagingDir string
}

// pushTree pushes children depth-first, then the manifest itself.
func (m *mirrorer) pushTree(ctx context.Context, raw []byte, mediaType string, refOrDigest string) error {
	var doc manifestDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	for _, child := range doc.Manifests {
		childRaw, childType, err := m.fetchManifest(ctx, child.Digest)
		if err != nil {
			return err
		}
		if actual := digestOf(childRaw); actual != child.Digest {
			return fmt.Errorf("child manifest %s served with digest %s", child.Digest, actual)
		}
		if childType == "" {
			childType = child.MediaType
		}
		if err := m.pushTree(ctx, childRaw, childType, child.Digest); err != nil {
			return err
		}
	}
	for _, b := range doc.blobs() {
		if err := m.mirrorBlob(ctx, b); err != nil {
			return err
		}
	}
	return m.dst.putManifest(ctx, m.dstRepo, raw, doc.mediaTypeOr(mediaType), refOrDigest)
}

func (m *mirrorer) fetchManifest(ctx context.Context, ref string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/v2/%s/manifests/%s", m.src.base, m.srcRepo, ref), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", manifestAccept)
	res, err := m.src.do(m.srcRepo, transport.PullScope, req)
	if err != nil {
		return nil, "", fmt.Errorf("source manifest %s: %w", ref, err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", statusError(fmt.Sprintf("source manifest %s", ref), res)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32*1024*1024))
	if err != nil {
		return nil, "", fmt.Errorf("source manifest %s: %w", ref, err)
	}
	return raw, res.Header.Get("Content-Type"), nil
}

// mirrorBlob streams a source blob to a temp file with inline digest
// verification, then pushes it (direct-to-storage for large blobs). Skipped
// entirely when the destination already has the digest.
func (m *mirrorer) mirrorBlob(ctx context.Context, d descriptor) error {
	exists, err := m.dst.blobExists(ctx, m.dstRepo, d.Digest, transport.PushScope)
	if err != nil {
		return err
	}
	if exists {
		zap.L().Debug("blob exists at destination", zap.String("digest", d.Digest))
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/v2/%s/blobs/%s", m.src.base, m.srcRepo, d.Digest), nil)
	if err != nil {
		return err
	}
	res, err := m.src.do(m.srcRepo, transport.PullScope, req)
	if err != nil {
		return fmt.Errorf("source blob %s: %w", d.Digest, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return statusError(fmt.Sprintf("source blob %s", d.Digest), res)
	}

	file, err := stagingFile(m.stagingDir, "contain-mirror-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name()) //nolint:errcheck
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hasher), res.Body)
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("source blob %s: %w", d.Digest, err)
	}
	if closeErr != nil {
		return closeErr
	}
	if err := verifyBlob(d, size, hasher); err != nil {
		return err
	}
	return m.dst.pushBlobFile(ctx, m.dstRepo, descriptor{Digest: d.Digest, Size: size}, file.Name())
}

func verifyBlob(d descriptor, size int64, hasher hash.Hash) error {
	if d.Size != 0 && size != d.Size {
		return fmt.Errorf("blob %s: source served %d bytes, descriptor says %d", d.Digest, size, d.Size)
	}
	actual := digestOfHash(hasher)
	if !strings.EqualFold(actual, d.Digest) {
		return fmt.Errorf("blob %s: source content hashes to %s", d.Digest, actual)
	}
	return nil
}

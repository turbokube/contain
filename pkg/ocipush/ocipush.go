// Package ocipush moves images into registries with digests preserved
// verbatim: manifests and blobs are transferred as raw bytes, never
// re-serialized (see the digest caveats with remote.WriteIndex noted in
// pkg/testcases).
//
// Three entry points share one registry client: Push sends an OCI image
// layout from disk, Mirror copies between registries verifying every node it
// forwards, and NewProxy serves a local registry endpoint that re-uploads
// what stock docker tooling pushes to it.
//
// Blobs at or above Options.ExtThreshold are uploaded through the
// _directpush/v1 extension (spec: PROTOCOL.md, attached) when the
// registry advertises it via OCI extensions discovery: the registry hands
// out presigned URLs and blob bytes go straight to object storage,
// bypassing proxy body-size limits. Registries without the extension get
// standard OCI monolithic blob uploads exclusively, for all blob sizes.
package ocipush

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"go.uber.org/zap"
)

const (
	// DefaultExtThreshold: blobs this size or larger prefer the direct-to-storage
	// extension API. Small blobs take the standard path (fewer round trips).
	DefaultExtThreshold    = 8 * 1024 * 1024
	mediaTypeImageManifest = "application/vnd.oci.image.manifest.v1+json"
)

var digestRe = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type Options struct {
	// Auth overrides keychain resolution (e.g. authn.Anonymous in tests).
	Auth authn.Authenticator
	// Keychain resolves registry credentials; defaults to authn.DefaultKeychain.
	Keychain authn.Keychain
	// ExtThreshold is the minimum blob size for direct-to-storage upload;
	// 0 means DefaultExtThreshold.
	ExtThreshold int64
	// PartSize proposed to the registry for multipart uploads; the registry
	// decides the actual part size. 0 lets the registry choose.
	PartSize int64
	// Transport overrides the default http transport.
	Transport http.RoundTripper
}

// descriptor is the subset of an OCI content descriptor we need for walking.
type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// manifestDoc is the union of OCI index and image manifest fields we walk.
type manifestDoc struct {
	MediaType string       `json:"mediaType"`
	Manifests []descriptor `json:"manifests"`
	Config    *descriptor  `json:"config"`
	Layers    []descriptor `json:"layers"`
}

// blobs is the config and layers in push order: a config is just another
// blob the manifest references, and both walkers need the same set.
func (d manifestDoc) blobs() []descriptor {
	if d.Config == nil {
		return d.Layers
	}
	return append([]descriptor{*d.Config}, d.Layers...)
}

// mediaTypeOr resolves what to send as Content-Type on the manifest PUT:
// what the referring descriptor or response said, else the document's own
// field, else the OCI image manifest type. Shared so the two walkers cannot
// drift on it.
func (d manifestDoc) mediaTypeOr(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if d.MediaType != "" {
		return d.MediaType
	}
	return mediaTypeImageManifest
}

// digestOf is the digest of raw manifest or blob bytes, in the string form
// the registry API and the extension payloads use.
func digestOf(b []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

// digestOfHash is digestOf for content that was streamed through a hash
// rather than held as bytes.
func digestOfHash(h hash.Hash) string {
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

// pusher walks an OCI layout and pushes to one repository.
type pusher struct {
	layout string
	repo   string
	c      *regClient
}

// Push pushes the OCI image layout at layoutDir to image (a tag or digest
// reference). A single-entry index.json pushes its entry as the image root
// (the docker buildx -o type=oci convention); a multi-entry index.json is
// itself pushed as the root index.
func Push(ctx context.Context, layoutDir string, image string, opts Options) error {
	ref, err := name.ParseReference(image)
	if err != nil {
		return fmt.Errorf("parse reference %s: %w", image, err)
	}
	c, err := newRegClient(ref.Context().Registry, opts)
	if err != nil {
		return err
	}
	p := &pusher{layout: layoutDir, repo: ref.Context().RepositoryStr(), c: c}

	indexBytes, err := os.ReadFile(filepath.Join(layoutDir, "index.json"))
	if err != nil {
		return fmt.Errorf("read layout index: %w", err)
	}
	var index manifestDoc
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return fmt.Errorf("parse layout index: %w", err)
	}
	if len(index.Manifests) == 0 {
		return fmt.Errorf("layout index.json has no manifests")
	}

	if len(index.Manifests) == 1 {
		root := index.Manifests[0]
		zap.L().Info("pushing", zap.String("ref", ref.String()), zap.String("root", root.Digest))
		return p.pushManifestDescriptor(ctx, root, ref.Identifier())
	}
	zap.L().Info("pushing multi-entry layout index as root",
		zap.String("ref", ref.String()), zap.Int("manifests", len(index.Manifests)))
	return p.pushManifestBytes(ctx, indexBytes, index.MediaType, ref.Identifier())
}

func (p *pusher) blobPath(digest string) (string, error) {
	if !digestRe.MatchString(digest) {
		return "", fmt.Errorf("unsupported digest: %s", digest)
	}
	return filepath.Join(p.layout, "blobs", "sha256", digest[len("sha256:"):]), nil
}

func (p *pusher) pushManifestDescriptor(ctx context.Context, d descriptor, refOrDigest string) error {
	path, err := p.blobPath(d.Digest)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest %s: %w", d.Digest, err)
	}
	// Child manifests are pushed by digest, so a registry would catch a
	// mismatch, but the root goes up by tag: an inconsistent layout would
	// otherwise be published under that tag unnoticed. Mirror verifies every
	// node it copies; a layout on disk deserves the same treatment, and it is
	// one hash of bytes already in memory.
	if actual := digestOf(raw); actual != d.Digest {
		return fmt.Errorf("layout manifest %s hashes to %s", d.Digest, actual)
	}
	return p.pushManifestBytes(ctx, raw, d.MediaType, refOrDigest)
}

// pushManifestBytes pushes children depth-first (blobs and nested manifests
// must exist before a manifest referencing them), then the manifest itself.
func (p *pusher) pushManifestBytes(ctx context.Context, raw []byte, mediaType string, refOrDigest string) error {
	var m manifestDoc
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	for _, child := range m.Manifests {
		if err := p.pushManifestDescriptor(ctx, child, child.Digest); err != nil {
			return err
		}
	}
	for _, b := range m.blobs() {
		if err := p.pushBlob(ctx, b); err != nil {
			return err
		}
	}
	return p.c.putManifest(ctx, p.repo, raw, m.mediaTypeOr(mediaType), refOrDigest)
}

func (p *pusher) pushBlob(ctx context.Context, d descriptor) error {
	path, err := p.blobPath(d.Digest)
	if err != nil {
		return err
	}
	return p.c.pushBlobFile(ctx, p.repo, d, path)
}

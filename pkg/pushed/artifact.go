package pushed

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"go.uber.org/zap"
)

// Artifact represents what we need to know (without manifest fetch) about the result of build+push
type Artifact struct {
	// Name without tag or digest used to reference the artifact in deployment resources
	ImageName string `json:"imageName"`
	// Ref here includes name and digest, i.e. the config Tag to push to (use .Http.Tag for image tag)
	TagRef string `json:"tag"`
	// MediaType is not part of skaffold's build output format
	MediaType types.MediaType `json:"mediaType"`
	// Platforms is not part of skaffold's build output format
	// But for multi-arch (index) images, neither skaffold nor buildctl writes platformsso we had to add it somewhere
	Platforms []v1.Platform `json:"platforms"`
	// BaseRef is the configured base image reference as provided (may include @sha256:digest)
	BaseRef string `json:"base,omitempty"`
	// reference is kept internally for reuse
	reference name.Reference
	// http is kept internally to assist http access
	hash v1.Hash
	// singleImageConfigHash is optional for BuildOutput and can't be reconstructed from JSON
	singleImageConfigHash v1.Hash
}

type ArtifactHttp struct {
	// Host is the registry host without protocol but with port
	Host string
	// Repository returns the path part of the image, excluding the /v2 http api prefix
	Repository string
	// Tag returns the tag name or "latest" if not specified
	Tag string
	// Hash returns digest, with algorithm and hex separable
	Hash v1.Hash
}

func (a *Artifact) Reference() name.Reference {
	return a.reference
}

func (a *Artifact) Http() ArtifactHttp {
	return ArtifactHttp{
		Host:       a.reference.Context().RegistryStr(),
		Repository: a.reference.Context().RepositoryStr(),
		Tag:        a.reference.Identifier(),
		Hash:       a.hash,
	}
}

// ConfigDigest returns the image config digest for single-image artifacts, or empty if unknown/not applicable.
func (a *Artifact) ConfigDigest() string {
	return a.singleImageConfigHash.String()
}

func newRef(tagRef string, hash v1.Hash) (*Artifact, error) {
	full := fmt.Sprintf("%s@%v", tagRef, hash)

	ref, err := reference.Parse(full)
	if err != nil {
		zap.L().Error("parse", zap.String("ref", full), zap.Error(err))
		return nil, err
	}
	named := ref.(reference.Named)
	if named == nil {
		zap.L().Error("named", zap.String("parsed", full), zap.String("ref", ref.String()))
	}

	// found no way to get default repo and tag from
	r, err := name.ParseReference(tagRef)
	if err != nil {
		zap.L().Error("parse", zap.String("ref", tagRef))
		return nil, err
	}

	// actually we can't use ref because it prepends default registry, skaffold probably doesn't do that
	return &Artifact{
		TagRef:    ref.String(),
		ImageName: named.Name(),
		reference: r,
		hash:      hash,
	}, nil
}

// NewArtifactSingleImage should be called for pushed image that has no index manifest
// with platform given by the build process
func NewSingleImage(tagRef string, digest v1.Hash, image v1.Image, platform *v1.Platform, baseRef string) (*Artifact, error) {
	if baseRef == "" {
		return nil, fmt.Errorf("baseRef is required")
	}
	a, err := newRef(tagRef, digest)
	if err != nil {
		return nil, err
	}

	// Get the image config digest
	configHash, err := image.ConfigName()
	if err != nil {
		zap.L().Warn("failed to get config digest", zap.Error(err))
	}
	a.singleImageConfigHash = configHash

	// Get the manifest for size and media type
	manifest, err := image.Manifest()
	if err != nil {
		zap.L().Warn("failed to get manifest", zap.Error(err))
	}

	a.MediaType = manifest.MediaType
	a.Platforms = []v1.Platform{*platform}
	a.BaseRef = baseRef

	return a, nil
}

// NewArtifact should be called for pushed image that is an index
// even if the index contains only a single platform
func NewIndexImage(tagRef string, digest v1.Hash, image v1.ImageIndex, baseRef string) (*Artifact, error) {
	if baseRef == "" {
		return nil, fmt.Errorf("baseRef is required")
	}
	a, err := newRef(tagRef, digest)
	if err != nil {
		return nil, err
	}

	// Get the manifest for size and media type
	manifest, err := image.IndexManifest()
	if err != nil {
		zap.L().Warn("failed to get manifest", zap.Error(err))
	}

	a.MediaType = manifest.MediaType
	a.Platforms = platformsFromIndexManifest(manifest)
	a.BaseRef = baseRef

	return a, nil
}

// UnmarshalJSON reconstructs internal state (reference and hash) from exported fields.
func (a *Artifact) UnmarshalJSON(data []byte) error {
	// Embedded alias pattern to inherit JSON tags and override platforms
	type artifactAlias Artifact
	type artifactJSON struct {
		artifactAlias
		Platforms []string `json:"platforms"`
	}
	var aux artifactJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Copy all exported fields from alias, then override Platforms
	*a = Artifact(aux.artifactAlias)
	a.Platforms = platformsFromStrings(aux.Platforms)

	// Reconstruct private fields from TagRef (which should be name[:tag]@digest)
	// Parse hash after '@'
	var base string
	var digestStr string
	if at := strings.LastIndex(a.TagRef, "@"); at != -1 {
		base = a.TagRef[:at]
		digestStr = a.TagRef[at+1:]
	} else {
		base = a.TagRef
	}

	if digestStr != "" {
		if h, err := v1.NewHash(digestStr); err == nil {
			a.hash = h
		} else {
			zap.L().Warn("failed to parse digest from tag", zap.String("tag", a.TagRef), zap.Error(err))
		}
	}

	if r, err := name.ParseReference(base); err == nil {
		a.reference = r
	} else {
		zap.L().Warn("failed to parse reference from tag", zap.String("tag", a.TagRef), zap.Error(err))
	}

	// singleImageConfigHash cannot be reconstructed from JSON; leave zero value
	return nil
}

// MarshalJSON encodes Platforms as strings (os/arch[/variant]) for readability/stability.
func (a Artifact) MarshalJSON() ([]byte, error) {
	// Use alias to reuse JSON tags for all fields, overriding only Platforms type
	type artifactAlias Artifact
	type artifactJSON struct {
		artifactAlias
		Platforms []string `json:"platforms"`
	}
	pf := make([]string, 0, len(a.Platforms))
	for _, p := range a.Platforms {
		pf = append(pf, p.String())
	}
	out := artifactJSON{
		artifactAlias: artifactAlias(a),
		Platforms:     pf,
	}
	return json.Marshal(out)
}

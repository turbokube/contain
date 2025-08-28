package multiarch

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"go.uber.org/zap"
)

// Pushed is a generalization of AppendResult because the latter seems to be per arch
type Pushed struct {
	Digest    v1.Hash
	MediaType types.MediaType
	Platforms []v1.Platform
}

func NewPushedNothing(err error) (Pushed, error) {
	return Pushed{}, err
}

// platformsFromIndexManifest returns a slice of platforms present in the index manifest.
// It includes only image manifests with a defined platform, skips attestations and non-image entries.
func platformsFromIndexManifest(idxm *v1.IndexManifest) []v1.Platform {
	platforms := make([]v1.Platform, 0, len(idxm.Manifests))
	for _, d := range idxm.Manifests {
		// Only include image manifests with a platform
		if d.Platform == nil {
			continue
		}
		if d.MediaType != types.OCIManifestSchema1 {
			// Skip non-image manifest entries (e.g., referrers/other types)
			continue
		}
		if d.Annotations != nil {
			if d.Platform.String() == AttestationPlatform && d.Annotations[ReferenceTypeAnnotation] == ReferenceTypeAttestation {
				// Skip attestation manifests
				continue
			}
		}
		platforms = append(platforms, *d.Platform)
	}
	return platforms
}

func NewPushedIndex(pushed v1.ImageIndex) (Pushed, error) {
	resultHash, err := pushed.Digest()
	if err != nil {
		zap.L().Error("index push image digest", zap.Error(err))
		return Pushed{}, err
	}
	mediaType, err := pushed.MediaType()
	if err != nil {
		zap.L().Error("index push image mediaType", zap.Error(err))
		return Pushed{}, err
	}
	// Collect platforms from the index manifest, excluding attestations and non-image entries
	idxm, err := pushed.IndexManifest()
	if err != nil {
		zap.L().Error("index push image indexManifest", zap.Error(err))
		return Pushed{}, err
	}
	// could we get platforms from the build process instead?
	platforms := platformsFromIndexManifest(idxm)
	return Pushed{
		Digest:    resultHash,
		MediaType: mediaType,
		Platforms: platforms,
	}, nil
}

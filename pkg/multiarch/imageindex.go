package multiarch

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/registry"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

type ImageIndex struct {
	baseRef      name.Digest
	prototypeRef *name.Digest
}

func NewFromMultiArchBase(config schema.ContainConfig, baseRegistry *registry.RegistryConfig) (*ImageIndex, error) {
	baseParsed, err := name.ParseReference(config.Base)
	if err != nil {
		return nil, err
	}

	baseRef, ok := baseParsed.(name.Digest)
	if !ok {
		return nil, fmt.Errorf("base without digest is currently de-supported, got %s", config.Base)
	}

	zap.L().Info("fetching", zap.Any("base", baseRef))
	base, err := remote.Get(baseRef, baseRegistry.CraneOptions.Remote...)
	if err != nil {
		return nil, err
	}

	if base.MediaType != types.OCIImageIndex {
		return nil, fmt.Errorf("currently only supports OCI index, got %s for %s", base.MediaType, config.Base)
	}

	baseIndex, err := base.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("image index from %s %s", base.MediaType, config.Base)
	}

	baseIndexManifest, err := baseIndex.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("image index manifest from %s %s", base.MediaType, config.Base)
	}

	requireMediaType := types.OCIManifestSchema1
	var prototypeRef *name.Digest
	for _, m := range baseIndexManifest.Manifests {
		if m.MediaType == requireMediaType {
			p := baseRef.Digest(m.Digest.String())
			prototypeRef = &p
			break
		}
	}
	if prototypeRef == nil {
		raw, err := baseIndex.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("raw manifest for debugging %v", err)
		}
		return nil, fmt.Errorf("found no manifest of type %s in index %s %v", requireMediaType, baseRef, raw)
	}

	return &ImageIndex{
		baseRef:      baseRef,
		prototypeRef: prototypeRef,
	}, nil
}

func (m *ImageIndex) GetPrototype() (name.Digest, error) {
	return *m.prototypeRef, nil
}

// WithPrototypeAppend should be called when appender has run on the prototype image
func (m *ImageIndex) WithPrototypeAppend(result appender.AppendResult) error {
	panic("TODO")
}

// PushIndex derives indexes from prototype + append and pushes them
// with the assumption that append has pushed all the layers
func (m *ImageIndex) PushIndex(registry *registry.RegistryConfig) error {
	return nil
}

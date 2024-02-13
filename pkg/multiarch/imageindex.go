package multiarch

import (
	"errors"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/registry"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// imageIndexItem is sufficient to produce a result manifest at the end
type imageIndexItem struct {
	base name.Digest
}

type ImageIndex struct {
	baseRef   name.Digest
	prototype *imageIndexItem
	remaining []*imageIndexItem
	append    *appender.AppendResult
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

	index := &ImageIndex{
		baseRef:   baseRef,
		remaining: make([]*imageIndexItem, 0),
	}

	requireMediaType := types.OCIManifestSchema1
	for i, m := range baseIndexManifest.Manifests {
		zap.L().Debug("index",
			zap.Int("item", i),
			zap.String("mediaType", string(m.MediaType)),
			zap.String("platform", m.Platform.String()),
		)
		if m.Platform == nil {
			zap.L().Info("skipping layer without platform",
				zap.String("got", string(m.MediaType)),
				zap.String("supported", string(m.MediaType)),
			)
			continue
		}
		if m.MediaType != requireMediaType {
			zap.L().Warn("skipping unsupported media type",
				zap.String("got", string(m.MediaType)),
				zap.String("supported", string(m.MediaType)),
			)
			continue
		}
		item := &imageIndexItem{
			base: index.baseRef.Digest(m.Digest.String()),
		}
		if index.prototype == nil {
			index.prototype = item
		} else {
			index.remaining = append(index.remaining, item)
		}
	}

	if index.prototype == nil {
		raw, err := baseIndex.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("raw manifest for debugging %v", err)
		}
		return nil, fmt.Errorf("found no platform manifest of type %s in index %s %v", requireMediaType, baseRef, raw)
	}
	// reminder: we're stricter than necessary in early iterations, to help standardize on index types
	if len(index.remaining) == 0 {
		raw, err := baseIndex.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("raw manifest for debugging %v", err)
		}
		return nil, fmt.Errorf("found only one platform manifest of type %s in index %s %v", requireMediaType, baseRef, raw)
	}

	return index, nil
}

func (m *ImageIndex) GetPrototypeBase() (name.Digest, error) {
	return m.prototype.base, nil
}

// WithPrototypeAppend should be called when appender has run on the prototype image
func (m *ImageIndex) WithPrototypeAppend(result appender.AppendResult) error {
	m.append = &result
	// TODO produce result manifests here, to validate result and bases
	// probably in the item impls
	// maybe we don't need the .append prop
	// remember platform
	return nil
}

// PushIndex derives indexes from prototype + append and pushes them
// with the assumption that append has pushed all the layers
func (m *ImageIndex) PushIndex(registry *registry.RegistryConfig) error {
	if m.append == nil {
		return errors.New("WithPrototypeAppend has not been called")
	}
	return nil
}

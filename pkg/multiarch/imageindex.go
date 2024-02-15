package multiarch

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
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
	baseRef    name.Digest
	prototype  *imageIndexItem
	remaining  []*imageIndexItem
	indexStart v1.ImageIndex
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

	// found no clone method on v1.ImageIndex so let's reuse the fetched one
	// (because empty.Index caused err at Push due to Image(Hash), i.e. manifest lookup, not implemented)
	index.indexStart = mutate.RemoveManifests(baseIndex, func(desc v1.Descriptor) bool {
		zap.L().Debug("clearing index",
			zap.String("platform", desc.Platform.String()),
			zap.String("digest", desc.Digest.String()),
		)
		// or do we want to keep attestation manifests?
		return true
	})

	return index, nil
}

func (m *ImageIndex) GetPrototypeBase() (name.Digest, error) {
	return m.prototype.base, nil
}

// PushIndex takes the AppendResult of the prototype contain
// and the original index to push a new multi-arch (i.e. multi-manifest) image
func (m *ImageIndex) PushIndex(tag name.Reference, result appender.AppendResult, config *registry.RegistryConfig) (v1.Hash, error) {

	append := []mutate.IndexAddendum{result.Pushed}
	// TODO produce and add the other manifests

	index := mutate.AppendManifests(m.indexStart, append...)

	hash, err := index.Digest()
	if err != nil {
		zap.L().Error("index digest", zap.Error(err))
		return v1.Hash{}, err
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		zap.L().Error("index manifest", zap.Error(err))
		return v1.Hash{}, err
	}
	zap.L().Info("index",
		zap.String("digest", hash.String()),
		zap.Int("manifests", len(manifest.Manifests)),
	)
	raw, err := index.RawManifest()
	if err != nil {
		zap.L().Error("raw manifest", zap.Error(err))
	}
	fmt.Println(string(raw))

	err = remote.Put(tag, index, config.CraneOptions.Remote...)
	if err != nil {
		zap.L().Error("put", zap.Error(err))
		return v1.Hash{}, err
	}

	return hash, nil
}

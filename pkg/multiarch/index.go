package multiarch

import (
	"bytes"
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

var noDigestYet = name.Digest{}

type IndexManifests struct {
	baseRef    name.Digest
	prototype  name.Digest
	pending    []*v1.Manifest
	indexStart v1.ImageIndex
}

func NewFromMultiArchBase(config schema.ContainConfig, baseRegistry *registry.RegistryConfig) (*IndexManifests, error) {
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

	index := &IndexManifests{
		baseRef: baseRef,
		pending: make([]*v1.Manifest, len(baseIndexManifest.Manifests)-1),
	}

	requireMediaType := types.OCIManifestSchema1
	for i, d := range baseIndexManifest.Manifests {
		zap.L().Debug("child descriptor",
			zap.Int("item", i),
			zap.String("mediaType", string(d.MediaType)),
			zap.String("platform", d.Platform.String()),
		)
		if d.Platform == nil {
			zap.L().Info("skipping layer without platform",
				zap.String("got", string(d.MediaType)),
				zap.String("supported", string(d.MediaType)),
			)
			continue
		}
		if d.MediaType != requireMediaType {
			zap.L().Warn("skipping unsupported media type",
				zap.String("got", string(d.MediaType)),
				zap.String("supported", string(d.MediaType)),
			)
			continue
		}
		if index.prototype == noDigestYet {
			index.prototype = index.baseRef.Digest(d.Digest.String())
		} else {
			index.pending[i-1], err = index.getChildManifest(index.baseRef, d, baseRegistry)
			if err != nil {
				zap.L().Error("index descriptor to manifest", zap.Error(err))
				return nil, err
			}
		}
	}

	if index.prototype == noDigestYet {
		raw, err := baseIndex.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("raw manifest for debugging %v", err)
		}
		return nil, fmt.Errorf("found no platform manifest of type %s in index %s %v", requireMediaType, baseRef, raw)
	}

	// reminder: we're stricter than necessary in early iterations, to help standardize on index types
	if len(index.pending) == 0 {
		raw, err := baseIndex.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("raw manifest for debugging %v", err)
		}
		return nil, fmt.Errorf("found only one platform manifest of type %s in index %s %v", requireMediaType, baseRef, raw)
	}

	// found no clone method on v1.ImageIndex so let's reuse the fetched one
	// (because empty.Index caused err at Push due to Image(Hash), i.e. manifest lookup, not implemented)
	// If reusing the original index turns out to be a bad idea we could start from empty.Index
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

func (m *IndexManifests) getChildManifest(baseRef name.Digest, manifest v1.Descriptor, config *registry.RegistryConfig) (*v1.Manifest, error) {
	ref := baseRef.Digest(manifest.Digest.String())
	// "current" here means the base's child manifest that we want to derive from
	current, err := remote.Get(ref, config.CraneOptions.Remote...)
	if err != nil {
		zap.L().Error("get current",
			zap.String("base", baseRef.String()),
			zap.String("child", ref.String()),
			zap.String("childtype", string(manifest.MediaType)),
			zap.Error(err),
		)
		return nil, err
	}
	// We can't use remote.Image() because of "If the fetched artifact is an index, it will attempt to resolve the index to a child image with the appropriate platform."
	// i.e. we must check media type using Head/Get first
	if current.MediaType.IsIndex() {
		zap.L().Error("get current",
			zap.String("base", baseRef.String()),
			zap.String("child", ref.String()),
			zap.String("childtype", string(manifest.MediaType)),
		)
		return nil, fmt.Errorf("unsupported nested index digest %s type %s", ref.String(), current.MediaType)
	}
	currentmanifest, err := v1.ParseManifest(bytes.NewReader(current.Manifest))
	if err != nil {
		zap.L().Error("parse current manifest",
			zap.String("base", baseRef.String()),
			zap.String("child", ref.String()),
			zap.String("childtype", string(manifest.MediaType)),
			zap.Error(err),
		)
		return nil, err
	}
	return currentmanifest, nil
}

func (m *IndexManifests) GetPrototypeBase() (name.Digest, error) {
	return m.prototype, nil
}

// PushIndex takes the AppendResult of the prototype contain
// and the original index to push a new multi-arch (i.e. multi-manifest) image
func (m *IndexManifests) PushIndex(tag name.Reference, result appender.AppendResult, config *registry.RegistryConfig) (v1.Hash, error) {

	layers := make([]v1.Descriptor, len(result.AddedManifestLayers))
	for _, prototypeAdded := range result.AddedManifestLayers {
		layers = append(layers, prototypeAdded.Descriptor())
	}

	indexAppend := []mutate.IndexAddendum{result.Pushed}

	for i, child := range m.pending {
		zap.L().Info("layers",
			zap.Any("child", child),
			zap.Int("existing", len(child.Layers)),
			zap.Int("add", len(layers)),
		)
		child.Layers = append(child.Layers, layers...)
		t, err := NewTaggableChild(*child)
		if err != nil {
			zap.L().Error("child taggable", zap.Int("index", i), zap.Error(err))
			return v1.Hash{}, err
		}
		err = remote.Put(tag, t, config.CraneOptions.Remote...)
		if err != nil {
			zap.L().Error("child put", zap.Error(err))
			return v1.Hash{}, err
		}
		indexAppend = append(indexAppend, mutate.IndexAddendum{
			Add: t,
		})
	}

	index := mutate.AppendManifests(m.indexStart, indexAppend...)

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

	taggable, err := NewTaggableIndex(index)
	if err != nil {
		zap.L().Error("taggable", zap.Error(err))
		return v1.Hash{}, err
	}

	err = remote.Put(tag, taggable, config.CraneOptions.Remote...)
	if err != nil {
		zap.L().Error("put", zap.Error(err))
		return v1.Hash{}, err
	}

	return hash, nil
}

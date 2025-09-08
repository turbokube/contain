package multiarch

import (
	"bytes"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/turbokube/contain/pkg/pushed"
	"github.com/turbokube/contain/pkg/registry"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

var noDigestYet = v1.Hash{}

type IndexManifests struct {
	baseRef    name.Digest
	toAppend   []ToAppend
	indexStart v1.ImageIndex
	prototype  *ToAppend
}

type ToAppend struct {
	// base is the ref for using a manifest item as base image
	base name.Digest
	// meta is the manifest item, but the digest is not known before contain
	meta *v1.Descriptor
	// baseManifest is the manifest of base, in case we need any information from there
	baseManifest *v1.Manifest
}

func newToAppend(baseRef name.Digest, manifestMeta v1.Descriptor) ToAppend {
	pendingDigest := manifestMeta.DeepCopy()
	pendingDigest.Digest = noDigestYet
	pendingDigest.Size = -1
	return ToAppend{
		base: baseRef.Digest(manifestMeta.Digest.String()),
		meta: pendingDigest,
	}
}

type EachAppend func(baseRef name.Digest, tagRef name.Reference, tagRegistry *registry.RegistryConfig) (mutate.IndexAddendum, error)

func NewFromMultiArchBase(config schema.ContainConfig, baseRegistry *registry.RegistryConfig) (*IndexManifests, error) {
	matchPlatforms, err := MatchPlatformsForAppend(config)
	if err != nil {
		return nil, err
	}

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

	// We parse index for single-platform builds as well, to make platform handling explicit.
	// To add support for non-index bases we'll probably use the default go-containerregistry behavior instead.
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
		baseRef:  baseRef,
		toAppend: make([]ToAppend, 0),
	}

	basePlatforms := make([]string, 0)
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
		} else {
			basePlatforms = append(basePlatforms, d.Platform.String())
		}
		if !matchPlatforms(d) {
			zap.L().Info("skipping layer excluded by platforms config",
				zap.String("platform", d.Platform.String()),
				zap.Strings("config", config.Platforms),
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
		if d.Annotations != nil {
			if d.Platform.String() == pushed.AttestationPlatform && d.Annotations[pushed.ReferenceTypeAnnotation] == pushed.ReferenceTypeAttestation {
				zap.L().Info("skipping attestation manifest",
					zap.String("reference", d.Annotations[pushed.ReferenceDigestAnnotation]),
				)
				continue
			}
		}
		base := newToAppend(index.baseRef, d)
		// we probably don't need prototype or pending (child manifests) given the deprecations below
		if index.prototype == nil {
			index.prototype = &base
		}
		base.baseManifest, err = index.getChildManifest(index.baseRef, d, baseRegistry)
		if err != nil {
			zap.L().Error("index descriptor to manifest", zap.Error(err))
			return nil, err
		}
		index.toAppend = append(index.toAppend, base)
	}

	if index.prototype == nil {
		raw, err := baseIndex.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("raw manifest for debugging %v", err)
		}
		zap.L().Error("manifest search",
			zap.Any("fetched", baseRef),
			zap.Strings("wanted", config.Platforms),
			zap.ByteString("raw", raw),
		)
		return nil, fmt.Errorf("found no platform manifest of type %s in index %s %v", requireMediaType, baseRef, basePlatforms)
	}

	// reminder: we're stricter than necessary in early iterations, to help standardize on index types
	if len(index.toAppend) == 0 {
		raw, err := baseIndex.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("raw manifest for debugging %v", err)
		}
		zap.L().Error("manifest", zap.ByteString("raw", raw))
		return nil, fmt.Errorf("found only one platform manifest of type %s in index %s %v", requireMediaType, baseRef, basePlatforms)
	}

	// found no clone method on v1.ImageIndex so let's reuse the fetched one
	// (because empty.Index caused err at Push due to Image(Hash), i.e. manifest lookup, not implemented)
	// If reusing the original index turns out to be a bad idea we could start from empty.Index
	index.indexStart = mutate.RemoveManifests(baseIndex, func(desc v1.Descriptor) bool {
		zap.L().Debug("index entry clear",
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

// GetPrototypeBase gets a single base to operate on as a prototype of how all archs/manifests should be mutated
// Deprecated: see EachAppend
func (m *IndexManifests) GetPrototypeBase() (name.Digest, error) {
	return m.prototype.base, nil
}

// SizeAppend is the number of manifests we'd append to
// in constrast to for example SizeBase = original size, SizeResult = in the index that will be pushed
func (m *IndexManifests) SizeAppend() int {
	return len(m.toAppend)
}

func (m *IndexManifests) BaseRef() name.Digest {
	return m.baseRef
}

func (m *IndexManifests) PushWithAppend(append EachAppend, tagRef name.Reference, tagRegistry *registry.RegistryConfig) (*pushed.Artifact, error) {
	var manifests = make([]mutate.IndexAddendum, len(m.toAppend))
	for i, c := range m.toAppend {
		if c.meta.Digest != noDigestYet {
			zap.L().Fatal("has digest already", zap.Int("item", i), zap.Any("toAppend", c))
		}
		var err error
		manifests[i], err = append(c.base, tagRef, tagRegistry)
		if err != nil {
			zap.L().Error("append", zap.Int("item", i), zap.Any("base", c), zap.Error(err))
			return nil, err
		}
	}
	resultIndex := mutate.AppendManifests(m.indexStart, manifests...)
	if resultIndex == nil {
		zap.L().Fatal("nil result from AppendManifests")
	}
	resultTaggable, err := NewTaggableIndex(resultIndex)
	if err != nil {
		zap.L().Error("taggable", zap.Any("index", resultIndex), zap.Error(err))
		return nil, err
	}
	for _, added := range manifests {
		zap.L().Debug("index entry addded",
			zap.String("platform", added.Platform.String()),
			zap.String("digest", added.Digest.String()),
		)
	}
	err = remote.Put(tagRef, resultTaggable, tagRegistry.CraneOptions.Remote...)
	if err != nil {
		zap.L().Error("index put", zap.Any("ref", tagRef), zap.Error(err))
		return nil, err
	}
	// Build artifact from the result index
	d, err := resultIndex.Digest()
	if err != nil {
		return nil, err
	}
	return pushed.NewIndexImage(tagRef.String(), d, resultIndex, m.baseRef.String())
}

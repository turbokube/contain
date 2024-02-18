package multiarch

import (
	"bytes"
	"encoding/json"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"go.uber.org/zap"
)

// TaggableChild allows remote.Put of an index's child manifest
// and holds enough metadata for the index to reference it
type TaggableChild struct {
	manifest  []byte
	digest    v1.Hash
	mediaType types.MediaType
	size      int64
}

// check that we implement the interfaces required for remote.Put
var _ remote.Taggable = (*TaggableChild)(nil)
var _ partial.Describable = (*TaggableChild)(nil)

func NewTaggableChild(manifest v1.Manifest) (TaggableChild, error) {
	rawManifest, err := json.MarshalIndent(manifest, "", "   ")
	if err != nil {
		zap.L().Error("raw manifest", zap.Error(err))
		return TaggableChild{}, err
	}
	digest, size, err := v1.SHA256(bytes.NewReader(rawManifest))
	if err != nil {
		zap.L().Error("raw manifest digest", zap.Error(err))
		return TaggableChild{}, err
	}
	return TaggableChild{
		manifest:  rawManifest,
		mediaType: manifest.MediaType,
		digest:    digest,
		size:      size,
	}, nil
}

func (t TaggableChild) RawManifest() ([]byte, error) {
	return t.manifest, nil
}

func (t TaggableChild) MediaType() (types.MediaType, error) {
	return t.mediaType, nil
}

func (t TaggableChild) Digest() (v1.Hash, error) {
	return t.digest, nil
}

func (t TaggableChild) Size() (int64, error) {
	return t.size, nil
}

package multiarch

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"go.uber.org/zap"
)

// TaggableIndex wraps v1.ImageIndex so that go-containerregistry remote.Put doesn't treat it as an image
type TaggableIndex struct {
	manifest  []byte
	digest    v1.Hash
	mediaType types.MediaType
	size      int64
}

// check that we implement the interfaces required for remote.Put
var _ remote.Taggable = (*TaggableIndex)(nil)
var _ partial.Describable = (*TaggableIndex)(nil)

// NewTaggableIndex extracts the necessary data immediately
// so later mutations won't affect the manifest
// and calls to interface methods won't err
func NewTaggableIndex(index v1.ImageIndex) (TaggableIndex, error) {
	manifest, err := index.RawManifest()
	if err != nil {
		zap.L().Error("raw manifest", zap.Error(err))
		return TaggableIndex{}, err
	}
	digest, err := index.Digest()
	if err != nil {
		zap.L().Error("digest", zap.Error(err))
		return TaggableIndex{}, err
	}
	mediaType, err := index.MediaType()
	if err != nil {
		zap.L().Error("mediatype", zap.Error(err))
		return TaggableIndex{}, err
	}
	size, err := index.Size()
	if err != nil {
		zap.L().Error("size", zap.Error(err))
		return TaggableIndex{}, err
	}
	return TaggableIndex{
		manifest:  manifest,
		digest:    digest,
		mediaType: mediaType,
		size:      size,
	}, nil
}

func (t TaggableIndex) RawManifest() ([]byte, error) {
	return t.manifest, nil
}

func (t TaggableIndex) Digest() (v1.Hash, error) {
	return t.digest, nil
}

func (t TaggableIndex) MediaType() (types.MediaType, error) {
	return t.mediaType, nil
}

func (t TaggableIndex) Size() (int64, error) {
	return t.size, nil
}

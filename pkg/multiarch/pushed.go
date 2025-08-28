package multiarch

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"go.uber.org/zap"
)

type Pushed struct {
	Digest    v1.Hash
	MediaType types.MediaType
}

func NewPushedNothing(err error) (Pushed, error) {
	return Pushed{}, err
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
	return Pushed{
		Digest:    resultHash,
		MediaType: mediaType,
	}, nil
}

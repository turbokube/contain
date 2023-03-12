package layers

import (
	"errors"

	"github.com/c9h-to/contain/pkg/localdir"
	schema "github.com/c9h-to/contain/pkg/schema/v1"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.uber.org/zap"
)

type LayerBuilder func() (v1.Layer, error)

func NewLayerBuilder(cfg schema.Layer) (LayerBuilder, error) {
	// TODO can we check that only one option is set
	// (this concept is modelled on skaffold's "build:" config)
	if cfg.LocalDir.Path != "" {
		return newLayerBuilderLocalDir(cfg.LocalDir)
	}
	return nil, errors.New("no layer builder config found")
}

func newLayerBuilderLocalDir(cfg schema.LocalDir) (LayerBuilder, error) {
	if len(cfg.Ignore) > 0 {
		zap.L().Fatal("Localdir ignore patterns not supported yet")
	}
	dir := localdir.Dir{
		Path:          cfg.Path,
		ContainerPath: localdir.NewPathMapperPrepend(cfg.ContainerPath),
	}
	return func() (v1.Layer, error) {
		return localdir.FromFilesystem(dir)
	}, nil
}

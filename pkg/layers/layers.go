package layers

import (
	"errors"
	"fmt"

	"github.com/c9h-to/contain/pkg/localdir"
	schema "github.com/c9h-to/contain/pkg/schema/v1"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
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
	dir := localdir.Dir{
		Path: cfg.Path,
	}
	if cfg.ContainerPath != "" {
		dir.ContainerPath = localdir.NewPathMapperPrepend(cfg.ContainerPath)
	} else {
		dir.ContainerPath = localdir.NewPathMapperAsIs()
	}
	if len(cfg.Ignore) > 0 {
		var err error
		dir.Ignore, err = patternmatcher.New(cfg.Ignore)
		if err != nil {
			return nil, fmt.Errorf("patternatcher from: %v", cfg.Ignore)
		}
	}
	if cfg.MaxFiles > 0 {
		dir.MaxFiles = cfg.MaxFiles
	}
	if cfg.MaxSize != "" {
		s, err := localdir.NewSize(cfg.MaxSize)
		if err != nil {
			return nil, err
		}
		dir.MaxSize = s
	}
	return func() (v1.Layer, error) {
		return localdir.FromFilesystem(dir)
	}, nil
}

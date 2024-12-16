package layers

import (
	"errors"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
	"github.com/turbokube/contain/pkg/localdir"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

type LayerBuilder func() (v1.Layer, error)

func NewLayerBuilder(cfg schema.Layer) (LayerBuilder, error) {
	// TODO can we check that only one option is set
	// (this concept is modelled on skaffold's "build:" config)
	if cfg.LocalFile.Path != "" {
		if cfg.LocalDir.Path != "" {
			return nil, errors.New("each layer item must have exactly one type, got localFile and localDir")
		}
		return configure(localdir.NewFile(), schema.LocalDir{
			Path:          cfg.LocalFile.Path,
			ContainerPath: cfg.LocalFile.ContainerPath,
			MaxSize:       cfg.LocalFile.MaxSize,
		}, cfg.Attributes)
	}
	if cfg.LocalDir.Path != "" {
		return configure(localdir.NewDir(), cfg.LocalDir, cfg.Attributes)
	}
	return nil, errors.New("no layer builder config found")
}

func configure(dir localdir.From, cfg schema.LocalDir, attributes schema.LayerAttributes) (LayerBuilder, error) {
	dir.Path = cfg.Path
	if cfg.ContainerPath != "" {
		dir.ContainerPath = localdir.NewPathMapperPrepend(cfg.ContainerPath)
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
		return localdir.FromFilesystem(dir, attributes)
	}, nil
}

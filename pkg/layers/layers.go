package layers

import (
	"errors"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
	"github.com/turbokube/contain/pkg/localdir"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

// LayerBuilder produces a layer for the given platform. Builders for
// platform-agnostic layers (localDir, localFile with only Path set) ignore
// the argument; builders for localFile.pathPerPlatform resolve the source
// path per call.
type LayerBuilder func(platform v1.Platform) (v1.Layer, error)

// Build invokes every builder for platform and returns the resulting
// layer slice. Callers that do not need per-platform resolution (for
// example sync to a running container) may pass the zero v1.Platform;
// that works for localDir and for localFile configs that only set Path.
func Build(builders []LayerBuilder, platform v1.Platform) ([]v1.Layer, error) {
	out := make([]v1.Layer, len(builders))
	for i, b := range builders {
		layer, err := b(platform)
		if err != nil {
			return nil, fmt.Errorf("layer %d for %s: %w", i, platform.String(), err)
		}
		out[i] = layer
	}
	return out, nil
}

func NewLayerBuilder(cfg schema.Layer) (LayerBuilder, error) {
	hasLocalFile := cfg.LocalFile.Path != "" || len(cfg.LocalFile.PathPerPlatform) > 0
	if hasLocalFile {
		if cfg.LocalDir.Path != "" {
			return nil, errors.New("each layer item must have exactly one type, got localFile and localDir")
		}
		return newLocalFileBuilder(cfg.LocalFile, cfg.Attributes)
	}
	if cfg.LocalDir.Path != "" {
		return configure(localdir.NewDir(), cfg.LocalDir, cfg.Attributes)
	}
	return nil, errors.New("no layer builder config found")
}

// newLocalFileBuilder returns a builder that resolves the source path for
// the requested platform on each invocation. This is the per-arch
// localFile path; for localFile configs with only Path set the closure
// still works (ResolveLocalFilePath returns Path regardless of platform).
func newLocalFileBuilder(lf schema.LocalFile, attributes schema.LayerAttributes) (LayerBuilder, error) {
	return func(platform v1.Platform) (v1.Layer, error) {
		resolved := schema.ResolveLocalFilePath(lf, platform)
		if resolved == "" {
			return nil, fmt.Errorf("localFile: no path for platform %s", platform.String())
		}
		inner, err := configure(localdir.NewFile(), schema.LocalDir{
			Path:          resolved,
			ContainerPath: lf.ContainerPath,
			MaxSize:       lf.MaxSize,
		}, attributes)
		if err != nil {
			return nil, err
		}
		return inner(platform)
	}, nil
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
	return func(_ v1.Platform) (v1.Layer, error) {
		return localdir.FromFilesystem(dir, attributes)
	}, nil
}

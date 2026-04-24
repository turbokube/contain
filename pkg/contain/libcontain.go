package contain

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/turbokube/contain/pkg/annotate"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/layers"
	"github.com/turbokube/contain/pkg/multiarch"
	"github.com/turbokube/contain/pkg/pushed"
	"github.com/turbokube/contain/pkg/pushlock"
	"github.com/turbokube/contain/pkg/registry"
	schemav1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// WriteOptions controls where the built image is written.
type WriteOptions struct {
	// Push enables pushing the image to the registry (default true for backwards compat).
	Push bool
	// OutputPath, if non-empty, writes the built image to this path.
	OutputPath string
	// OutputFormat selects the output format: "tarball" (default) or "oci".
	OutputFormat OutputFormat
	// PushLock, if non-nil, serializes push operations across processes.
	PushLock pushlock.PushLock
}

// Run is what you call if you have a complete config and want to push an artifact
// - Depends on a zap.ReplaceGlobals logger
// - No side effects other than push to config.Tag (and child tags in case of an index)
// - Not affected by environment, i.e. config defines a repeatable build
func Run(config schemav1.ContainConfig) (*pushed.Artifact, error) {

	// index, err := multiarch.NewRequireMultiArchBase(config)
	// if err != nil {
	// 	return nil, err
	// }
	layers, err := RunLayers(config)
	if err != nil {
		return nil, err
	}
	output, err := RunAppend(config, layers, WriteOptions{Push: true})
	if err != nil {
		return nil, err
	}
	if output.Skaffold != nil && len(output.Skaffold.Builds) > 0 {
		return &output.Skaffold.Builds[0], nil
	}
	return nil, fmt.Errorf("no build output available")
}

// RunLayers is the file system access part of a run
func RunLayers(config schemav1.ContainConfig) ([]v1.Layer, error) {

	layerBuilders := make([]layers.LayerBuilder, len(config.Layers))
	for i, layerCfg := range config.Layers {
		b, err := layers.NewLayerBuilder(layerCfg)
		if err != nil {
			zap.L().Error("Failed to get layer builder",
				zap.Int("index", i),
				zap.Any("config", layerCfg),
				zap.Error(err),
			)
			return nil, err
		}
		layerBuilders[i] = b
	}

	layers := make([]v1.Layer, len(layerBuilders))
	for i, builder := range layerBuilders {
		layer, err := builder()
		if err != nil {
			zap.L().Error("layer builder invocation failed", zap.Error(err))
			return nil, err
		}
		layers[i] = layer
	}

	return layers, nil

}

// Removed NewPushedSingleImage: producers now return *pushed.Artifact directly.

// RunAppend is the remote access part of a run
func RunAppend(config schemav1.ContainConfig, layers []v1.Layer, opts WriteOptions) (*pushed.BuildOutput, error) {
	// source repo can differ from destination repo, we should probably struct tag + remote config
	var baseRegistry *registry.RegistryConfig
	var tagRegistry *registry.RegistryConfig

	baseRegistry, err := registry.New(config)
	if err != nil {
		zap.L().Error("registry", zap.Error(err))
		return nil, err
	}
	tagRegistry = baseRegistry

	if config.Tag == "" {
		zap.L().Fatal("requires config tag")
	}
	buildOutputTag, err := name.ParseReference(config.Tag)
	if err != nil {
		zap.L().Error("tag", zap.Error(err))
		return nil, err
	}

	// currently we assume that config base is an index
	index, err := multiarch.NewFromMultiArchBase(config, baseRegistry)
	if err != nil {
		zap.L().Error("index", zap.Error(err))
		return nil, err
	}

	each := func(b name.Digest, t name.Reference, tr *registry.RegistryConfig, _ v1.Platform) (mutate.IndexAddendum, error) {
		a, err := appender.New(b, tr, t)
		if err != nil {
			zap.L().Error("appender", zap.Error(err))
			return mutate.IndexAddendum{}, err
		}
		a.WithSkipPush(!opts.Push)
		if opts.PushLock != nil {
			a.WithPushLock(opts.PushLock)
		}
		// Apply env overrides/additions if configured
		if len(config.Env) > 0 {
			var envs []string
			for _, e := range config.Env {
				// simple validation: skip empties
				if e.Name == "" {
					continue
				}
				envs = append(envs, fmt.Sprintf("%s=%s", e.Name, e.Value))
			}
			a.WithEnvs(envs)
		}
		// Process entrypoint/args overrides
		if len(config.Entrypoint) > 0 || len(config.Args) > 0 {
			a.WithEntrypointArgs(config.Entrypoint, config.Args)
		}
		// Set base image annotation hints as per crane rebase docs
		if ann, err := annotate.NewBaseImageAnnotations(config.Base); err == nil {
			a.WithAnnotate(ann)
		} else {
			zap.L().Error("base image annotations", zap.Error(err))
		}
		r, err := a.Append(layers...)
		if err != nil {
			zap.L().Error("append", zap.Error(err))
			return mutate.IndexAddendum{}, err
		}
		return r.Pushed, nil
	}

	var result *pushed.Artifact
	var resultImg v1.Image
	var resultIdx v1.ImageIndex

	if index.SizeAppend() > 1 {
		resultIdx, result, err = index.BuildWithAppend(each, buildOutputTag, tagRegistry, opts.Push)
		if err != nil {
			zap.L().Error("index build", zap.Error(err))
			return nil, err
		}
	} else {
		if len(config.Platforms) > index.SizeAppend() {
			return nil, fmt.Errorf("found %d index manifests to append to, config has %d platforms", index.SizeAppend(), len(config.Platforms))
		}
		prototypeBase, err := index.GetPrototypeBase()
		if err != nil {
			return nil, fmt.Errorf("single platform base: %w", err)
		}
		pushedAdd, err := each(prototypeBase, buildOutputTag, tagRegistry, index.PrototypePlatform())
		if err != nil {
			zap.L().Error("single image build", zap.Error(err))
			return nil, err
		}
		// Build artifact for single-image case
		resultImg, _ = pushedAdd.Add.(v1.Image)
		hash, err := pushedAdd.Add.Digest()
		if err != nil {
			return nil, err
		}
		result, err = pushed.NewSingleImage(buildOutputTag.String(), hash, resultImg, pushedAdd.Descriptor.Platform, config.Base)
		if err != nil {
			return nil, err
		}
		zap.L().Info("single platform", zap.String("tag", buildOutputTag.String()), zap.String("hash", hash.String()))
	}

	if opts.OutputPath != "" {
		format := opts.OutputFormat
		if format == "" {
			format = FormatTarball
		}
		if err := writeOutput(format, opts.OutputPath, buildOutputTag, resultImg, resultIdx); err != nil {
			zap.L().Error("output", zap.String("format", string(format)), zap.Error(err))
			return nil, err
		}
	}

	// Propagate base information from config to the pushed artifact
	// BaseRef already set by constructors

	// todo multi-arch index from prototype result to result index
	// produces new result hash

	// Build output from the produced artifact (includes config digest for single images)
	buildOutput, err := pushed.NewBuildOutput(buildOutputTag.String(), result)
	if err != nil {
		zap.L().Error("buildOutput", zap.Error(err))
		return nil, err
	}

	return buildOutput, nil

}

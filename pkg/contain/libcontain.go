package contain

import (
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/layers"
	"github.com/turbokube/contain/pkg/multiarch"
	"github.com/turbokube/contain/pkg/registry"
	schemav1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// Run is what you call if you have a complete config and want to push an artifact
// - Depends on a zap.ReplaceGlobals logger
// - No side effects other than push to config.Tag (and child tags in case of an index)
// - Not affected by environment, i.e. config defines a repeatable build
func Run(config schemav1.ContainConfig) (*Artifact, error) {

	// index, err := multiarch.NewRequireMultiArchBase(config)
	// if err != nil {
	// 	return nil, err
	// }
	layers, err := RunLayers(config)
	if err != nil {
		return nil, err
	}
	output, err := RunAppend(config, layers)
	if err != nil {
		return nil, err
	}
	return &output.Builds[0], nil
}

// RunLayers is the file system access part of a run
func RunLayers(config schemav1.ContainConfig) ([]v1.Layer, error) {

	layerBuilders := make([]layers.LayerBuilder, len(config.Layers))
	for i, layerCfg := range config.Layers {
		b, err := layers.NewLayerBuilder(layerCfg)
		if err != nil {
			zap.L().Error("Failed to get layer builder",
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

// RunAppend is the remote access part of a run
func RunAppend(config schemav1.ContainConfig, layers []v1.Layer) (*BuildOutput, error) {
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

	each := func(b name.Digest, t name.Reference, tr *registry.RegistryConfig) (mutate.IndexAddendum, error) {
		a, err := appender.New(b, tr, t)
		if err != nil {
			zap.L().Error("appender", zap.Error(err))
			return mutate.IndexAddendum{}, err
		}
		// todo WithAnnotate?
		r, err := a.Append(layers...)
		if err != nil {
			zap.L().Error("append", zap.Error(err))
			return mutate.IndexAddendum{}, err
		}
		return r.Pushed, nil
	}

	resultHash, err := index.PushWithAppend(each, buildOutputTag, tagRegistry)
	if err != nil {
		zap.L().Error("index push", zap.Error(err))
		return nil, err
	}

	// todo multi-arch index from prototype result to result index
	// produces new result hash

	buildOutput, err := NewBuildOutput(buildOutputTag.String(), resultHash)
	if err != nil {
		zap.L().Fatal("buildOutput", zap.Error(err))
		return nil, err
	}

	return buildOutput, nil

}

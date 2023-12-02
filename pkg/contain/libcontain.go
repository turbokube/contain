package contain

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/layers"
	schemav1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

// Run uses the provided config for a complete contain run
// and returns the resulting image with digest
// - Depends on a zap.ReplaceGlobals logger
// - Should not access env
func Run(config schemav1.ContainConfig) (*Artifact, error) {
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

func RunLayers(config schemav1.ContainConfig) ([]v1.Layer, error) {

	layerBuilders := make([]layers.LayerBuilder, len(config.Layers))
	for i, layerCfg := range config.Layers {
		b, err := layers.NewLayerBuilder(layerCfg)
		if err != nil {
			zap.L().Fatal("Failed to get layer builder",
				zap.Any("config", layerCfg),
				zap.Error(err),
			)
		}
		layerBuilders[i] = b
	}

	layers := make([]v1.Layer, len(layerBuilders))
	for i, builder := range layerBuilders {
		layer, err := builder()
		if err != nil {
			zap.L().Fatal("layer builder invocation failed", zap.Error(err))
		}
		layers[i] = layer
	}

	return layers, nil

}

func RunAppend(config schemav1.ContainConfig, layers []v1.Layer) (*BuildOutput, error) {

	a, err := appender.New(config)
	if err != nil {
		zap.L().Fatal("intialization", zap.Error(err))
	}

	if config.Tag == "" {
		zap.L().Fatal("requires config tag")
	}
	hash, err := a.Append(layers...)
	if err != nil {
		zap.L().Fatal("append", zap.Error(err))
		return nil, err
	}

	buildOutput, err := NewBuildOutput(config.Tag, hash)
	if err != nil {
		zap.L().Fatal("buildOutput", zap.Error(err))
		return nil, err
	}

	return buildOutput, nil

}

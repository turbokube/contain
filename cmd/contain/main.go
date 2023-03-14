package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/c9h-to/contain/pkg/contain"
	"github.com/c9h-to/contain/pkg/layers"
	"github.com/c9h-to/contain/pkg/schema"
	schemav1 "github.com/c9h-to/contain/pkg/schema/v1"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	base string
)

func init() {
	flag.StringVar(&base,
		"b",
		"",
		"base image (implies tag = $IMAGE, local dir = $PWD, container path = /app)",
	)
	flag.Parse()
}

func main() {
	consoleDebugging := zapcore.Lock(os.Stdout)
	consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	consoleEnabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return true
	})
	core := zapcore.NewCore(consoleEncoder, consoleDebugging, consoleEnabler)
	logger := zap.New(core)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	var err error

	workdir := flag.Arg(0)
	if workdir != "" && workdir != "." && workdir != "./" {
		workdir, err = filepath.Abs(workdir)
		if err != nil {
			zap.L().Fatal("absolute path", zap.String("arg", flag.Arg(0)), zap.Error(err))
		}
		stat, err := os.Stat(workdir)
		if err != nil {
			zap.L().Fatal("context path not found",
				zap.String("arg", flag.Arg(0)),
				zap.String("abs", workdir),
				zap.Error(err),
			)
		}
		if !stat.IsDir() {
			zap.L().Fatal("context path not a directory",
				zap.String("arg", flag.Arg(0)),
				zap.String("abs", workdir),
			)
		}
		chdir := contain.NewChdir(workdir)
		defer chdir.Cleanup()
	}

	var config schemav1.ContainConfig
	if base != "" {
		zap.L().Info("got base arg", zap.String("base", base))
		// TODO last arg should be CWD, defaults to PWD
		config = schema.TemplateApp(base)
	} else {
		config, err = schema.ParseConfig("contain.yaml")
		if err != nil {
			zap.L().Fatal("Can't start without config", zap.Error(err))
		}
	}

	if config.Tag == "" {
		image, exists := os.LookupEnv("IMAGE")
		if exists {
			zap.L().Debug("read IMAGE env", zap.String("tag", image))
			config.Tag = image
		}
	}

	if len(config.Platforms) == 0 {
		platforms, exists := os.LookupEnv("PLATFORMS")
		if exists {
			p := strings.Split(platforms, ",")
			zap.L().Debug("read PLATFORMS env", zap.Strings("platforms", p))
			config.Platforms = p
		}
	}

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

	c, err := contain.NewContain(&config)
	if err != nil {
		zap.L().Fatal("intialization", zap.Error(err))
	}

	layers := make([]v1.Layer, len(layerBuilders))
	for i, builder := range layerBuilders {
		layer, err := builder()
		if err != nil {
			zap.L().Fatal("layer builder invocation failed", zap.Error(err))
		}
		layers[i] = layer
	}

	c.Append(layers...)
}

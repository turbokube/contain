package main

import (
	"flag"
	"fmt"
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
	BUILD      = "development"
	configPath = "contain.yaml"
	helpStream = os.Stderr
	version    bool
	help       bool
	base       string
)

func init() {
	flag.BoolVar(&version, "version", false, "print build version")
	flag.BoolVar(&help, "help", false, "print usage")
	flag.StringVar(&base,
		"b",
		"",
		"base image (implies tag = $IMAGE, local dir = $PWD, container path = /app)",
	)
	flag.Usage = func() {
		fmt.Fprintf(helpStream, "contain version: %s\n", BUILD)
		fmt.Fprintf(helpStream, "\nUsage: contain [context path]\n")
		fmt.Fprintf(helpStream, "Context path contains a file %s or the flag -b must be provided\n", configPath)
		fmt.Fprintf(helpStream, "\n")
		flag.PrintDefaults()
	}
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

	if version {
		fmt.Fprintf(helpStream, "%s\n", BUILD)
		os.Exit(0)
	}
	if help {
		flag.Usage()
		os.Exit(0)
	}

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
		config = schema.TemplateApp(base)
	} else {
		config, err = schema.ParseConfig(configPath)
		if err != nil {
			flag.Usage()
			zap.L().Fatal("start requires config or base + env", zap.Error(err))
		}
	}

	if config.Tag == "" {
		image, exists := os.LookupEnv("IMAGE")
		if exists {
			zap.L().Debug("read IMAGE env", zap.String("tag", image))
			config.Tag = image
		} else {
			repo, repoExists := os.LookupEnv("IMAGE_REPO")
			rtag, rtagExists := os.LookupEnv("IMAGE_TAG")
			if repoExists && rtagExists {
				config.Tag = fmt.Sprintf("%s:%s", repo, rtag)
				zap.L().Debug("read IMAGE_REPO and IMAGE_TAG env", zap.String("tag", config.Tag))
			}
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

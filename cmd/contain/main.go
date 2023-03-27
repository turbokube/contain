package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/turbokube/contain/pkg/contain"
	"github.com/turbokube/contain/pkg/layers"
	"github.com/turbokube/contain/pkg/run"
	"github.com/turbokube/contain/pkg/schema"
	schemav1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	BUILD        = "development"
	helpStream   = os.Stderr
	version      bool
	help         bool
	debug        bool
	watch        bool
	configPath   string
	base         string
	runSelector  string
	runNamespace string
)

func init() {
	flag.BoolVar(&version, "version", false, "print build version")
	flag.BoolVar(&help, "help", false, "print usage")
	flag.BoolVar(&debug, "x", false, "logs at debug level")
	flag.StringVar(&configPath,
		"c",
		"contain.yaml",
		"config file path relative to context dir, or - for stdin",
	)
	flag.StringVar(&base,
		"b",
		"",
		"base image (implies tag = $IMAGE, local dir = $PWD, container path = /app)",
	)
	flag.StringVar(&runSelector,
		"r",
		"",
		"append to running container instead of to base image, pod selector",
	)
	flag.StringVar(&runNamespace,
		"n",
		"",
		"namespace for run, if empty current context is used",
	)
	flag.BoolVar(&watch,
		"w",
		false,
		"watch layers sources and trigger build/run on change",
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
	consoleDebugging := zapcore.Lock(os.Stderr)
	consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	consoleEnabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return debug || lvl != zapcore.DebugLevel
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

	if watch {
		zap.L().Fatal("watch not implemented")
	}

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

	if base == "" && os.Getenv("CONTAIN_BASE") != "" {
		base = os.Getenv("CONTAIN_BASE")
		zap.L().Debug("base from env")
	}

	var config schemav1.ContainConfig
	config, err = schema.ParseConfig(configPath)
	if err != nil {
		// TODO we should probably distinguish between different types of config errors first
		zap.L().Debug("config parse failed, expected if invoked with -b",
			zap.Error(err),
			zap.String("-b", base),
		)
		if base == "" {
			flag.Usage()
			zap.L().Fatal("start requires config or base + env", zap.Error(err))
		}
		zap.L().Info("config from template", zap.String("base", base))
		config = schema.TemplateApp(base)
	} else {
		// How does skaffold deal with config yaml defaults and with overrides from CLI? Just code, or something more clever?
		if base != "" {
			if config.Base != "" {
				config.Status.Overrides.Base = true
				zap.L().Debug("config parsed, base overridden", zap.String("base", base))
			} else {
				zap.L().Debug("config parsed, base set", zap.String("base", base))
			}
			config.Base = base
		} else {
			zap.L().Debug("config parsed", zap.String("base", config.Base))
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

	var aboutConfig = make([]zap.Field, 0)
	if config.Status.Template {
		aboutConfig = append(aboutConfig, zap.Bool("templated", config.Status.Template))
	} else {
		aboutConfig = append(aboutConfig,
			zap.String("md5", config.Status.Md5),
			zap.String("sha256", config.Status.Sha256),
		)
	}
	if config.Status.Overrides.Base {
		aboutConfig = append(aboutConfig, zap.Bool("overriddenBase", true))
	}
	if workdir, err := os.Getwd(); err == nil {
		aboutConfig = append(aboutConfig, zap.String("workdir", workdir))
	}

	zap.L().Info("config", aboutConfig...)

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

	if runSelector != "" {
		if len(config.Platforms) != 0 {
			zap.L().Warn("platforms not supported for run")
		}
		config.Sync = schema.TemplateSync(runNamespace, runSelector)
		sync, err := run.NewContainersync(&config)
		if err != nil {
			zap.L().Fatal("containersync init", zap.Error(err))
		}
		target, err := sync.Run(layers...)
		if err != nil {
			zap.L().Fatal("containersync run", zap.Error(err))
		}
		zap.L().Info("containersync completed")
		fmt.Printf(`{"namespace":"%s","pod":"%s",container:"%s"}%s`, target.Pod.Namespace, target.Pod.Name, target.Container.Name, "\n")
		return
	}

	if config.Tag == "" {
		zap.L().Error("append requires IMAGE env or config")
	}
	hash, err := c.Append(layers...)
	if err != nil {
		zap.L().Fatal("append", zap.Error(err))
	}

	fmt.Printf("%s@%v\n", config.Tag, hash)
}

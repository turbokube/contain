package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/contain"
	"github.com/turbokube/contain/pkg/run"
	"github.com/turbokube/contain/pkg/schema"
	schemav1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	envPlatforms = "PLATFORMS"
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
	fileOutput   string
	metadataFile string
	platformsEnv bool
	tStart       = time.Now()
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
	flag.StringVar(&fileOutput,
		"file-output",
		"",
		"produce a builds JSON like Skaffold does",
	)
	flag.StringVar(&metadataFile,
		"metadata-file",
		"",
		"produce a metadata JSON like buildctl does",
	)
	flag.BoolVar(&platformsEnv,
		"platforms-env-require",
		false,
		fmt.Sprintf("requires env %s to be set, unless config specifies platforms", envPlatforms),
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

func writeBuildOutput(buildOutput *contain.BuildOutput) {
	if fileOutput != "" {
		f, err := os.OpenFile(fileOutput, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			wd, _ := os.Getwd()
			zap.L().Fatal("file-output open", zap.String("cwd", wd), zap.String("path", fileOutput), zap.Error(err))
		}
		if writeErr := buildOutput.WriteSkaffoldJSON(f); writeErr != nil {
			wd, _ := os.Getwd()
			zap.L().Fatal("file-output write", zap.String("cwd", wd), zap.String("path", fileOutput), zap.Error(writeErr))
		}
	}
	if metadataFile != "" {
		f, err := os.OpenFile(metadataFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			wd, _ := os.Getwd()
			zap.L().Fatal("metadata-file open", zap.String("cwd", wd), zap.String("path", metadataFile), zap.Error(err))
		}
		if writeErr := buildOutput.WriteBuildctlJSON(f); writeErr != nil {
			wd, _ := os.Getwd()
			zap.L().Fatal("metadata-file write", zap.String("cwd", wd), zap.String("path", metadataFile), zap.Error(writeErr))
		}
	}
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

	writeBuildOutput(&contain.BuildOutput{
		Trace: &contain.BuildTrace{
			Start: &tStart,
		},
	})

	workdir := flag.Arg(0)
	var chdir *appender.Chdir
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
		chdir = appender.NewChdir(workdir)
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
			zap.String("path", configPath),
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
			} else {
				zap.L().Fatal("config tag must be set, or env IMAGE, or envs IMAGE_REPO and IMAGE_TAG")
			}
		}
	}

	platforms, exists := os.LookupEnv(envPlatforms)
	if exists {
		p := strings.Split(platforms, ",")
		zap.L().Debug("env", zap.String("name", envPlatforms), zap.Strings("platforms", p))
		if len(config.Platforms) == 0 {
			config.Platforms = p
		} else if !slices.Equal(config.Platforms, p) {
			zap.L().Info("platforms not equal, config kept", zap.String("env", platforms), zap.Strings("config", config.Platforms))
		}
	} else if platformsEnv {
		zap.S().Fatalf("%s env required but not found", envPlatforms)
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

	layers, err := contain.RunLayers(config)
	if err != nil {
		zap.L().Fatal("layers", zap.Error(err))
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

	buildOutput, err := contain.RunAppend(config, layers)
	if err != nil {
		zap.L().Fatal("append", zap.Error(err))
	}
	tEnd := time.Now()

	buildOutput.Trace = &contain.BuildTrace{
		Start: &tStart,
		End:   &tEnd,
		Env:   contain.BuildTraceEnv(os.Environ()),
	}
	buildOutput.Print()

	if chdir != nil {
		chdir.Cleanup()
	}

	writeBuildOutput(buildOutput)

}

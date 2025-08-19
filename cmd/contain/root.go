package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

const envPlatforms = "PLATFORMS"

var (
	BUILD   = "development"
	debug   bool
	version bool
	// timing
	tStart = time.Now()
	// build flags
	configPath   string
	base         string
	runSelector  string
	runNamespace string
	watch        bool
	fileOutput   string
	metadataFile string
	platformsEnv bool
	// sbom flags
	sbomBuildMetadata string
)

var rootCmd = &cobra.Command{
	Use:          "contain",
	Short:        "contain image build tool",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error { // default to build
		return runBuild(args)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&debug, "x", "x", false, "logs at debug level")
	rootCmd.PersistentFlags().BoolVar(&version, "version", false, "print build version and exit")

	rootCmd.AddCommand(newBuildCmd())
	rootCmd.AddCommand(newSbomCmd())
}

func newBuildCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "build [context path]",
		Short: "Build/append layers into an image",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errors.New("too many args: at most one context path")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error { return runBuild(args) },
	}
	c.Flags().StringVarP(&configPath, "c", "c", "contain.yaml", "config file path relative to context dir, or - for stdin")
	c.Flags().StringVarP(&base, "b", "b", "", "base image (implies tag = $IMAGE, local dir = $PWD, container path = /app)")
	c.Flags().StringVarP(&runSelector, "r", "r", "", "append to running container instead of to base image, pod selector")
	c.Flags().StringVarP(&runNamespace, "n", "n", "", "namespace for run, if empty current context is used")
	c.Flags().BoolVarP(&watch, "w", "w", false, "watch layers sources and trigger build/run on change")
	c.Flags().StringVar(&fileOutput, "file-output", "", "produce a builds JSON like Skaffold does")
	c.Flags().StringVar(&metadataFile, "metadata-file", "", "produce a metadata JSON like buildctl does")
	c.Flags().BoolVar(&platformsEnv, "platforms-env-require", false, fmt.Sprintf("requires env %s to be set, unless config specifies platforms", envPlatforms))
	return c
}

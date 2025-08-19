package main

import (
	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/sbom"
	"go.uber.org/zap"
)

// sbom command flags
var (
	sbomBuildMetadata string
	sbomInFile        string
)

func newSbomCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sbom",
		Short: "Generate (stub) SBOM output using build metadata",
		RunE:  runSbom,
	}
	c.Flags().StringVar(&sbomBuildMetadata, "metadata-file", "", "path to build metadata file (from skaffold/buildct/contain)")
	c.Flags().StringVar(&sbomInFile, "in", "", "path to SPDX file for the contents of the build")

	c.MarkFlagRequired("metadata-file") //nolint:errcheck
	return c
}

// runSbom sets up logging and executes the sbom generation logic.
func runSbom(cmd *cobra.Command, args []string) error { //nolint:revive,unused
	logger := newLogger()
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	return sbom.Generate(sbomBuildMetadata)
}

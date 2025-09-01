package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/pushed"
	"github.com/turbokube/contain/pkg/sbom"
	"go.uber.org/zap"
)

// sbom command flags
var (
	sbomBuildArtifacts string
	sbomInFile         string
	sbomOutFile        string
)

func newSbomCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sbom",
		Short: "Generate (stub) SBOM output using build metadata",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSbom,
	}
	c.Flags().StringVar(&sbomBuildArtifacts, "build-artifacts", "", "path to build metadata file (from skaffold/buildct/contain)")
	c.Flags().StringVar(&sbomInFile, "in", "", "path to SPDX file for the contents of the build")
	c.Flags().StringVar(&sbomOutFile, "out", "", "path to SPDX file to write (same as in to overwrite)")
	return c
}

// runSbom sets up logging and executes the sbom generation logic.
func runSbom(cmd *cobra.Command, args []string) error { //nolint:revive,unused
	logger := newLogger()
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	// If no args/flags provided, show help and exit cleanly
	if cmd.Flags().NFlag() == 0 && len(args) == 0 {
		_ = cmd.Help()
		return nil
	}

	// If an output path is provided positionally, use it unless --out is set
	if sbomOutFile == "" && len(args) > 0 {
		sbomOutFile = args[0]
	}

	// Basic validation
	if sbomInFile == "" {
		return fmt.Errorf("--in is required (path to input SPDX json)")
	}

	toolVersion := BUILD
	var artifact *pushed.Artifact // TODO read from sbomBuildArtifacts

	if err := sbom.WrapSPDX(sbomBuildArtifacts, sbomInFile, sbomOutFile, artifact, toolVersion); err != nil {
		return err
	}
	return nil
}

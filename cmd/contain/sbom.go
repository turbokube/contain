package main

import (
	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/sbom"
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
		RunE:  func(cmd *cobra.Command, _ []string) error { return sbom.Generate(sbomBuildMetadata) },
	}
	c.Flags().StringVar(&sbomBuildMetadata, "metadata-file", "", "path to build metadata file (from skaffold/buildct/contain)")
	c.Flags().StringVar(&sbomInFile, "in", "", "path to SPDX file for the contents of the build")

	c.MarkFlagRequired("metadata-file") //nolint:errcheck
	return c
}

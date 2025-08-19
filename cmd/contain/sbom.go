package main

import (
	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/sbom"
)

func newSbomCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sbom",
		Short: "Generate (stub) SBOM output using build metadata",
		RunE:  func(cmd *cobra.Command, _ []string) error { return sbom.Generate(sbomBuildMetadata) },
	}
	c.Flags().StringVar(&sbomBuildMetadata, "build-metadata", "", "path to build metadata file (required)")
	c.MarkFlagRequired("build-metadata") //nolint:errcheck
	return c
}

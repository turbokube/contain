package main

import (
	"github.com/spf13/cobra"
)

var (
	BUILD       = "development"
	debug       bool
	version     bool
	implicitRoot bool // set when root invoked without subcommand
)

var rootCmd = &cobra.Command{
	Use:          "contain",
	Short:        "contain image build tool",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error { // default to build
		implicitRoot = true
		return runBuild(args)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&debug, "x", "x", false, "logs at debug level")
	rootCmd.PersistentFlags().BoolVar(&version, "version", false, "print build version and exit")

	rootCmd.AddCommand(newBuildCmd())
	rootCmd.AddCommand(newSbomCmd())
}
	// build subcommand is defined in build.go via newBuildCmd()

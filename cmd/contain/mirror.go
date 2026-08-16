package main

import (
	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/ocipush"
	"go.uber.org/zap"
)

var (
	mirrorSrcPlainHTTP bool
	mirrorDstOptions   ocipush.Options
)

func newMirrorCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mirror <src-image> <dst-image>",
		Short: "Copy an image between registries, preserving digests",
		Long: `Copies an image (single manifest or full index) from one registry to
another, like crane cp. Manifests and blobs transfer as raw bytes and every
node is digest-verified before it is pushed, so even a plain-http source
cannot inject content under a wrong digest. The source reference may pin a
digest (repo:tag@sha256:... or repo@sha256:...).

Push uses the destination's direct-to-storage extension for large blobs
when available (see contain push). Credentials resolve like docker/crane.`,
		Args: cobra.ExactArgs(2),
		RunE: runMirror,
	}
	c.Flags().BoolVar(&mirrorSrcPlainHTTP, "src-plain-http", false,
		"use plain http for the source registry (e.g. cluster-internal registries)")
	addDirectUploadFlags(c, &mirrorDstOptions, "destination")
	addStagingFlag(c, &mirrorDstOptions)
	return c
}

func runMirror(cmd *cobra.Command, args []string) error {
	logger := newLogger()
	defer logger.Sync() //nolint:errcheck
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	return ocipush.Mirror(cmd.Context(), args[0], args[1], ocipush.MirrorOptions{
		Src: ocipush.SourceOptions{PlainHTTP: mirrorSrcPlainHTTP},
		Dst: mirrorDstOptions,
	})
}

package main

import (
	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/ocipush"
	"go.uber.org/zap"
)

// push command flags
var (
	pushPartSize     int64
	pushExtThreshold int64
)

func newPushCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "push <oci-layout-dir> <image>",
		Short: "Push an OCI image layout to a registry, preserving digests",
		Long: `Pushes an OCI image layout directory (e.g. from docker buildx build
-o type=oci,tar=false,dest=DIR) to a registry. Manifests and blobs are
transferred as raw bytes so all digests are preserved verbatim.

Registries that implement the direct-to-storage upload extension (such as
Yolean's Cloudflare R2 registry, where proxied uploads are size-limited)
receive large blobs via presigned URLs straight to object storage; other
registries get standard OCI uploads.

Credentials are resolved like docker/crane (docker login).`,
		Args: cobra.ExactArgs(2),
		RunE: runPush,
	}
	c.Flags().Int64Var(&pushExtThreshold, "direct-threshold", ocipush.DefaultExtThreshold,
		"minimum blob size in bytes for direct-to-storage upload")
	c.Flags().Int64Var(&pushPartSize, "part-size", 0,
		"multipart part size in bytes to propose to the registry (0 = registry default)")
	return c
}

func runPush(cmd *cobra.Command, args []string) error {
	logger := newLogger()
	defer logger.Sync() //nolint:errcheck
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	return ocipush.Push(cmd.Context(), args[0], args[1], ocipush.Options{
		ExtThreshold: pushExtThreshold,
		PartSize:     pushPartSize,
	})
}

package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/ocipush"
)

// addDirectUploadFlags registers the direct-to-storage upload options shared
// by push, mirror and registry-proxy, binding straight into the Options the
// command will pass to ocipush. target names the registry the options apply
// to, which is the only thing that differed between the three copies of this.
func addDirectUploadFlags(c *cobra.Command, opts *ocipush.Options, target string) {
	c.Flags().Int64Var(&opts.ExtThreshold, "direct-threshold", ocipush.DefaultExtThreshold,
		"minimum blob size in bytes for direct-to-storage upload")
	c.Flags().Int64Var(&opts.PartSize, "part-size", 0,
		fmt.Sprintf("multipart part size in bytes to propose to the %s (0 = registry default)", target))
}

package contain_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"dagger.io/dagger"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/contain"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/slices"
)

// cases is an array because a testcase may depend on an output image from an earlier testcase
var cases = []testcases.Testcase{
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("root.txt", "r")
			return schema.ContainConfig{
				Base: "contain-test/multiarch-base:noattest",
				Tag:  "contain-test/root:dot",
				Layers: []schema.Layer{
					{
						LocalDir: schema.LocalDir{
							Path:          ".",
							ContainerPath: "/app",
						},
					},
				},
			}
		},
		ExpectDigest: "sha256:---TODO-repeatable-test-builds----------------------------------",
		ExpectDagger: func(ref string, client *dagger.Client, t *testing.T, ctx context.Context) {

			resp, err := http.Head(fmt.Sprintf("http://%s/v2/", testRegistry))
			if err != nil {
				t.Error(err)
			}
			if resp.Status != "200 OK" {
				t.Errorf("%s %s", testRegistry, resp.Status)
			}
			fmt.Printf("#x %s OK\n", testRegistry)

			zap.L().Debug("dagger", zap.String("ref", ref))
			c := client.Container().From(ref)
			app, err := c.Rootfs().Directory("app").Entries(ctx)
			if err != nil {
				t.Error(err)
			}
			zap.L().Info("/app", zap.Int("entries", len(app)))
			if !slices.Contains(app, "root.txt") {
				t.Error("missing root.txt")
			}
			r := c.Rootfs().File("root.txt")
			if r == nil {
				t.Error("read root.txt")
			}
		},
	},
	{
		RunConfig: func(config *testcases.TestInput, dir *testcases.TempDir) schema.ContainConfig {
			dir.Write("root.txt", "r")
			return schema.ContainConfig{
				Base: "contain-test/multiarch-base:noattest",
				Tag:  "contain-test/root:nodot",
				Layers: []schema.Layer{
					{
						LocalDir: schema.LocalDir{
							Path:          "",
							ContainerPath: "/app",
						},
					},
				},
			}
		},
		ExpectDigest: "sha256:---TODO-repeatable-test-builds----------------------------------",
		ExpectDagger: nil,
	},
}

// func runner(testcase testcases.Testcase) func(t *testing.T) {
// 	return func(t *testing.T) {

// 	}
// }

func TestTestcases(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	t.Run(fmt.Sprintf("#- %s", testRegistry), func(t *testing.T) {
		resp, err := http.Head(fmt.Sprintf("http://%s/v2/", testRegistry))
		if err != nil {
			t.Error(err)
		}
		if resp.Status != "200 OK" {
			t.Errorf("%s %s", testRegistry, resp.Status)
		}
		fmt.Printf("#- %s OK\n", testRegistry)
	})

	ctx := context.Background()

	fmt.Printf("# cases: %d\n", len(cases))
	for i, testcase := range cases {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			// logs an initial zap entry because the ordering of test output might be confusing
			zap.L().Debug("DEBUG", zap.Int("case", i))
			if len(testcase.ExpectDigest) != 71 {
				t.Errorf("digest %s", testcase.ExpectDigest)
			}
			dir := testcases.NewTempDir(t)
			c := testcase.RunConfig(nil, dir)

			// this output is helpful in combination with dagger output
			fmt.Printf("\n#%d %s -> %s\n", i, c.Base, c.Tag)

			c.Base = fmt.Sprintf("%s/%s", testRegistry, c.Base)
			c.Tag = fmt.Sprintf("%s/%s", testRegistry, c.Tag)

			chdir := appender.NewChdir(dir.Root())
			defer chdir.Cleanup()

			// result, err := contain.Run(c)
			// if err != nil {
			// 	t.Errorf("libcontain run %v", err)
			// }
			// Use separate invocations to simplify debugging

			layers, err := contain.RunLayers(c)
			if err != nil {
				t.Errorf("layers %v", err)
			}
			zap.L().Debug("testcase layers", zap.Int("count", len(layers)))
			buildOutput, err := contain.RunAppend(c, layers)
			if err != nil {
				t.Errorf("append %v", err)
			}
			result := buildOutput.Builds[0]

			expectRef := fmt.Sprintf("%s@%s", c.Tag, testcase.ExpectDigest)
			if result.Tag != expectRef {
				if testcase.ExpectDagger != nil {
					client := testcases.DaggerClient(t, ctx)
					testcase.ExpectDagger(testcases.DaggerRef(c.Tag), client, t, ctx)
				}
				t.Errorf("pushed   %s\n                   expected %s", result.Tag, expectRef)
			}
		})
		// fmt.Printf("## CASE: %d\n", i)
		// r := runner(testcase)
		// t.Run(fmt.Sprintf("testcase %d", i), r)
		// t.Errorf("err %d\n", i)
	}
}

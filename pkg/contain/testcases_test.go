package contain_test

import (
	"fmt"
	"net/http"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/contain"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
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
		Expect: func(ref contain.Artifact, t *testing.T) {

			amd64 := v1.Platform{Architecture: "amd64", OS: "linux"}
			amd64options := append(testCraneOptions.Remote, remote.WithPlatform(amd64))
			amd64img, err := remote.Image(ref.Reference(), amd64options...)
			if err != nil {
				t.Error(err)
			}
			amd64layers, err := amd64img.Layers()
			if err != nil {
				t.Error(err)
			}

			zap.L().Debug("amd64", zap.Int("layers", len(amd64layers)))
			// https://github.com/google/go-containerregistry/blob/55ffb0092afd1313edad861a553b4fcea21b4da2/pkg/crane/export.go#L27
			// or Extract
			// filesystem or layer to tar
			// abstraction on top of tar?
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
		Expect: func(ref contain.Artifact, t *testing.T) {
			fmt.Printf("TODO expect %s\n", ref.Tag)
		},
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
				if testcase.Expect == nil {
					t.Error("missing Expect func")
				} else {
					testcase.Expect(result, t)
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

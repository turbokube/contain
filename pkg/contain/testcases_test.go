package contain_test

import (
	"fmt"
	"testing"

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
		ExpectDigest: "sha256:a",
		ExpectDagger: nil,
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
		ExpectDigest: "sha256:a",
		ExpectDagger: nil,
	},
}

func runner(testcase testcases.Testcase) func(t *testing.T) {
	return func(t *testing.T) {
		if len(testcase.ExpectDigest) != 71 {
			t.Errorf("digest %s", testcase.ExpectDigest)
		}
		dir := testcases.NewTempDir(t)
		c := testcase.RunConfig(nil, dir)
		c.Base = fmt.Sprintf("%s/%s", testRegistry, c.Base)
		c.Tag = fmt.Sprintf("%s/%s", testRegistry, c.Tag)
		t.Logf("%s -> %s", c.Base, c.Tag)
		result, err := contain.Run(c)
		if err != nil {
			t.Error(err)
		}
		expectRef := fmt.Sprintf("%s@%s", c.Tag, testcase.ExpectDigest)
		if result.Tag != expectRef {
			t.Errorf("pushed %s expected %s", result.Tag, expectRef)
			// TODO run expect
		}
	}
}

func TestCases(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	for i, testcase := range cases {
		r := runner(testcase)
		t.Run(fmt.Sprintf("testcase %d", i), r)
	}
}

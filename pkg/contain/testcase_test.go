package contain_test

import (
	"fmt"

	"dagger.io/dagger"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

type Testcase struct {
	RunConfig func(testRegistry string) *schema.ContainConfig
	// ExpectDigest is a sha256:-prefixed string, which if it matches the pushed digest passes test test with no further expect calls
	ExpectDigest string
	ExpectDagger func(*dagger.Client)
}

var testcases = []Testcase{
	{
		RunConfig: func(testRegistry string) *schema.ContainConfig {
			return &schema.ContainConfig{
				Base: fmt.Sprintf("%s/contain-test/multiarch-base:noattest"),
				Layers: []schema.Layer{
					{
						LocalDir: ""// TODO help define files for layers for tests,
					},
				},
			}
		},
		ExpectDigest: "sha256:a",
		ExpectDagger: nil,
	},
}

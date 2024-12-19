package contain_test

import (
	"testing"

	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/contain"
)

func TestBuildTraceEnv(t *testing.T) {
	RegisterTestingT(t)
	env := contain.BuildTraceEnv([]string{
		"FOO=bar",
		"CIX=baz",
		"CI=true",
		"TURBO=nosuffix",
		"TURBO_HASH=abc123",
		"IMAGE=img:123",
		"IMAGE_NAME=img",
	})
	Expect(env).NotTo(HaveKey("FOO"))
	Expect(env).NotTo(HaveKey("CIX"))
	Expect(env).To(HaveKeyWithValue("CI", "true"))
	Expect(env).NotTo(HaveKey("TURBO"))
	Expect(env).To(HaveKeyWithValue("TURBO_HASH", "abc123"))
	Expect(env).To(HaveKeyWithValue("IMAGE", "img:123"))
	Expect(env).To(HaveKeyWithValue("IMAGE_NAME", "img"))
}

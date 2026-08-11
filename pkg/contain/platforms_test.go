package contain_test

import (
	"fmt"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/contain"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
)

// platformsBase is the two-platform index (linux/amd64 and linux/arm64,
// neither with a variant) also used by the pathPerPlatform tests.
const platformsBase = pathPerPlatformBase

func platformsTestConfig(t *testing.T, tag string, platforms []string) (schema.ContainConfig, *testcases.TempDir) {
	t.Helper()
	dir := testcases.NewTempDir(t)
	writeTestFile(t, dir, "payload.txt", "PAYLOAD")

	cfg := schema.ContainConfig{
		Base:      fmt.Sprintf("%s/%s", testRegistry, platformsBase),
		Tag:       fmt.Sprintf("%s/%s", testRegistry, tag),
		Platforms: platforms,
		Layers: []schema.Layer{{
			LocalFile: schema.LocalFile{
				Path:          "payload.txt",
				ContainerPath: "/payload",
			},
		}},
	}
	return cfg, dir
}

// A platform in the config that matched nothing in the base index must fail
// the build. Before this check the guard only ran when at most one manifest
// matched, so asking for three platforms against a two-platform base pushed
// two of them and exited 0.
func TestPlatformsRequestedButMissingFromBase(t *testing.T) {
	RegisterTestingT(t)
	// the test registry persists pushes across runs, so the "nothing was
	// pushed" assertion below needs a tag that cannot already exist
	cfg, dir := platformsTestConfig(t, "contain-test/platforms:missing-"+testcases.RandomHex(8),
		[]string{"linux/amd64", "linux/arm64", "linux/s390x"})

	chdir := appender.NewChdir(dir.Root())
	defer chdir.Cleanup()

	builders, err := contain.RunLayers(cfg)
	Expect(err).NotTo(HaveOccurred())
	_, err = contain.RunAppend(cfg, builders, contain.WriteOptions{Push: true})
	Expect(err).To(HaveOccurred(), "expected a build failure for the unmatched platform")
	Expect(err.Error()).To(ContainSubstring("linux/s390x"), "error must name the platform that matched nothing")
	Expect(err.Error()).To(ContainSubstring("linux/arm64"), "error must show what the base does offer")

	_, headErr := crane.Head(cfg.Tag, crane.WithAuth(nil))
	Expect(headErr).To(HaveOccurred(), "nothing may be pushed when a requested platform is missing")
}

// The mirror case of the request in the arch-support report: the base spells
// arm64 without a variant, the config spells it with one. Matching is exact
// today, so this is a failure, and the error has to name both spellings
// rather than leaving the caller to diff two log lines.
func TestPlatformsVariantSpellingMismatchIsExplained(t *testing.T) {
	RegisterTestingT(t)
	cfg, dir := platformsTestConfig(t, "contain-test/platforms:variant",
		[]string{"linux/amd64", "linux/arm64/v8"})

	chdir := appender.NewChdir(dir.Root())
	defer chdir.Cleanup()

	builders, err := contain.RunLayers(cfg)
	Expect(err).NotTo(HaveOccurred())
	_, err = contain.RunAppend(cfg, builders, contain.WriteOptions{Push: true})
	Expect(err).To(HaveOccurred(), "linux/arm64/v8 does not match a base child declaring linux/arm64")
	Expect(err.Error()).To(ContainSubstring("linux/arm64/v8"), "error must name the requested spelling")
	Expect(err.Error()).To(ContainSubstring("linux/arm64"), "error must name the base's spelling")
}

// Requesting exactly what the base offers still builds every platform.
func TestPlatformsAllRequestedArePresent(t *testing.T) {
	RegisterTestingT(t)
	cfg, dir := platformsTestConfig(t, "contain-test/platforms:complete",
		[]string{"linux/amd64", "linux/arm64"})

	chdir := appender.NewChdir(dir.Root())
	defer chdir.Cleanup()

	builders, err := contain.RunLayers(cfg)
	Expect(err).NotTo(HaveOccurred())
	out, err := contain.RunAppend(cfg, builders, contain.WriteOptions{Push: true})
	Expect(err).NotTo(HaveOccurred())

	artifact := out.Artifact()
	got := make([]string, 0, len(artifact.Platforms))
	for _, p := range artifact.Platforms {
		got = append(got, p.String())
	}
	Expect(got).To(ConsistOf("linux/amd64", "linux/arm64"))
}

// A subset of the base index is a valid request, the result index then has
// only the requested platform.
func TestPlatformsSubsetOfBase(t *testing.T) {
	RegisterTestingT(t)
	cfg, dir := platformsTestConfig(t, "contain-test/platforms:subset",
		[]string{"linux/arm64"})

	chdir := appender.NewChdir(dir.Root())
	defer chdir.Cleanup()

	builders, err := contain.RunLayers(cfg)
	Expect(err).NotTo(HaveOccurred())
	out, err := contain.RunAppend(cfg, builders, contain.WriteOptions{Push: true})
	Expect(err).NotTo(HaveOccurred())

	artifact := out.Artifact()
	Expect(artifact.Platforms).To(HaveLen(1))
	Expect(artifact.Platforms[0].String()).To(Equal("linux/arm64"))
}

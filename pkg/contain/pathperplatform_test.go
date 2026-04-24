package contain_test

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/contain"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
)

// baseimage-multiarch1:noattest is a two-platform index: linux/amd64 and
// linux/arm64. That makes it a realistic stand-in for a distroless static
// base when exercising per-arch path resolution.
const pathPerPlatformBase = "contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4"

// fileInPlatformManifest pulls the arch-specific manifest from an index,
// extracts its single appended layer, and returns the tar body at
// containerPath. Fails the test if containerPath is not present.
func fileInPlatformManifest(t *testing.T, indexRef string, platform v1.Platform, containerPath string) string {
	t.Helper()
	digest, err := crane.Digest(indexRef, crane.WithPlatform(&platform), crane.WithAuthFromKeychain(nil), crane.WithAuth(nil))
	Expect(err).NotTo(HaveOccurred())

	// Parse reference of the arch manifest (index@digest).
	withDigest := strings.SplitN(indexRef, "@", 2)[0] + "@" + digest
	img, err := crane.Pull(withDigest, crane.WithPlatform(&platform), crane.WithAuth(nil))
	Expect(err).NotTo(HaveOccurred())

	rc := mutate.Extract(img)
	defer rc.Close()
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if hdr.Name == containerPath {
			buf := new(strings.Builder)
			if _, err := io.Copy(buf, tr); err != nil {
				t.Fatalf("copy: %v", err)
			}
			return buf.String()
		}
	}
	t.Fatalf("%s not found in %s for %s", containerPath, indexRef, platform.String())
	return ""
}

// writeFile writes body to dir/name and returns the absolute path, for use
// with RunConfig's TempDir.
func writeTestFile(t *testing.T, dir *testcases.TempDir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir.Root(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return name
}

func TestPathPerPlatform_DifferentBytesPerArch(t *testing.T) {
	RegisterTestingT(t)
	dir := testcases.NewTempDir(t)
	amdName := writeTestFile(t, dir, "amd64.bin", "AMD64-BODY")
	armName := writeTestFile(t, dir, "arm64.bin", "ARM64-BODY")

	cfg := schema.ContainConfig{
		Base: pathPerPlatformBase,
		Tag:  "contain-test/pathperplatform:distinct",
		Platforms: []string{"linux/amd64", "linux/arm64"},
		Layers: []schema.Layer{{
			LocalFile: schema.LocalFile{
				PathPerPlatform: map[string]string{
					"linux/amd64": amdName,
					"linux/arm64": armName,
				},
				ContainerPath: "/usr/local/bin/mybinary",
			},
		}},
	}
	cfg.Base = fmt.Sprintf("%s/%s", testRegistry, cfg.Base)
	cfg.Tag = fmt.Sprintf("%s/%s", testRegistry, cfg.Tag)

	chdir := appender.NewChdir(dir.Root())
	defer chdir.Cleanup()

	builders, err := contain.RunLayers(cfg)
	Expect(err).NotTo(HaveOccurred())
	out, err := contain.RunAppend(cfg, builders, contain.WriteOptions{Push: true})
	Expect(err).NotTo(HaveOccurred())
	Expect(out).NotTo(BeNil())

	artifact := out.Artifact()
	ref := artifact.Reference()
	amdBody := fileInPlatformManifest(t, ref.String(), v1.Platform{OS: "linux", Architecture: "amd64"}, "/usr/local/bin/mybinary")
	armBody := fileInPlatformManifest(t, ref.String(), v1.Platform{OS: "linux", Architecture: "arm64"}, "/usr/local/bin/mybinary")

	Expect(amdBody).To(Equal("AMD64-BODY"))
	Expect(armBody).To(Equal("ARM64-BODY"))

	// The per-arch layer digests must differ.
	amdImg, err := remote.Image(ref, append(testCraneOptions.Remote, remote.WithPlatform(v1.Platform{OS: "linux", Architecture: "amd64"}))...)
	Expect(err).NotTo(HaveOccurred())
	armImg, err := remote.Image(ref, append(testCraneOptions.Remote, remote.WithPlatform(v1.Platform{OS: "linux", Architecture: "arm64"}))...)
	Expect(err).NotTo(HaveOccurred())

	amdDigest, err := amdImg.Digest()
	Expect(err).NotTo(HaveOccurred())
	armDigest, err := armImg.Digest()
	Expect(err).NotTo(HaveOccurred())
	Expect(amdDigest).NotTo(Equal(armDigest), "per-arch manifests must differ")
}

func TestPathPerPlatform_FallbackPathCoversUnlistedArch(t *testing.T) {
	RegisterTestingT(t)
	dir := testcases.NewTempDir(t)
	writeTestFile(t, dir, "amd64.bin", "AMD64-BODY")
	writeTestFile(t, dir, "fallback.bin", "FALLBACK-BODY")

	cfg := schema.ContainConfig{
		Base: pathPerPlatformBase,
		Tag:  "contain-test/pathperplatform:fallback",
		Platforms: []string{"linux/amd64", "linux/arm64"},
		Layers: []schema.Layer{{
			LocalFile: schema.LocalFile{
				Path: "fallback.bin",
				PathPerPlatform: map[string]string{
					"linux/amd64": "amd64.bin",
				},
				ContainerPath: "/usr/local/bin/mybinary",
			},
		}},
	}
	cfg.Base = fmt.Sprintf("%s/%s", testRegistry, cfg.Base)
	cfg.Tag = fmt.Sprintf("%s/%s", testRegistry, cfg.Tag)

	chdir := appender.NewChdir(dir.Root())
	defer chdir.Cleanup()

	builders, err := contain.RunLayers(cfg)
	Expect(err).NotTo(HaveOccurred())
	out, err := contain.RunAppend(cfg, builders, contain.WriteOptions{Push: true})
	Expect(err).NotTo(HaveOccurred())

	artifact := out.Artifact()
	ref := artifact.Reference().String()
	amdBody := fileInPlatformManifest(t, ref, v1.Platform{OS: "linux", Architecture: "amd64"}, "/usr/local/bin/mybinary")
	armBody := fileInPlatformManifest(t, ref, v1.Platform{OS: "linux", Architecture: "arm64"}, "/usr/local/bin/mybinary")

	Expect(amdBody).To(Equal("AMD64-BODY"))
	Expect(armBody).To(Equal("FALLBACK-BODY"))
}

func TestPathPerPlatform_MissingPlatformErrorsBeforeAnyPush(t *testing.T) {
	RegisterTestingT(t)
	dir := testcases.NewTempDir(t)
	writeTestFile(t, dir, "amd64.bin", "AMD64-BODY")

	const tag = "contain-test/pathperplatform:missing"
	cfg := schema.ContainConfig{
		Base:      pathPerPlatformBase,
		Tag:       tag,
		Platforms: []string{"linux/amd64", "linux/arm64"},
		Layers: []schema.Layer{{
			LocalFile: schema.LocalFile{
				PathPerPlatform: map[string]string{
					"linux/amd64": "amd64.bin",
				},
				ContainerPath: "/usr/local/bin/mybinary",
			},
		}},
	}
	cfg.Base = fmt.Sprintf("%s/%s", testRegistry, cfg.Base)
	cfg.Tag = fmt.Sprintf("%s/%s", testRegistry, cfg.Tag)

	chdir := appender.NewChdir(dir.Root())
	defer chdir.Cleanup()

	builders, err := contain.RunLayers(cfg)
	Expect(err).NotTo(HaveOccurred())
	_, err = contain.RunAppend(cfg, builders, contain.WriteOptions{Push: true})
	Expect(err).To(HaveOccurred(), "expected missing-platform error")
	Expect(err.Error()).To(ContainSubstring("linux/arm64"), "error should name the missing platform")
	Expect(err.Error()).To(ContainSubstring("pathPerPlatform"), "error should suggest the fix")

	// No manifest should have been pushed under the target tag.
	_, headErr := crane.Head(cfg.Tag, crane.WithAuth(nil))
	Expect(headErr).To(HaveOccurred(), "tag must not exist after a failed validation")
}

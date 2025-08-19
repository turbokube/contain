package contain_test

import (
    "encoding/json"
    "fmt"
    "os"
    "strings"
    "testing"

    "github.com/google/go-containerregistry/pkg/v1/remote"
    . "github.com/onsi/gomega"
    "github.com/turbokube/contain/pkg/contain"
    schema "github.com/turbokube/contain/pkg/schema/v1"
    "github.com/turbokube/contain/pkg/testcases"
)

// TestBaseImageAnnotationsWithOverride verifies annotations when override env is set.
func TestBaseImageAnnotationsWithOverride(t *testing.T) {
    RegisterTestingT(t)
    dir := testcases.NewTempDir(t)
    dir.Write("annotate.txt", "data")

    // Use a single platform build to simplify fetching the image manifest.
    cfg := schema.ContainConfig{
        Base: "contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4",
        Tag:  "contain-test/annotate:base",
        Layers: []schema.Layer{{LocalDir: schema.LocalDir{Path: ".", ContainerPath: "/app"}}},
        Platforms: []string{"linux/amd64"},
    }
    cfg.Base = fmt.Sprintf("%s/%s", testRegistry, cfg.Base)
    cfg.Tag = fmt.Sprintf("%s/%s", testRegistry, cfg.Tag)

    layers, err := contain.RunLayers(cfg)
    Expect(err).NotTo(HaveOccurred())
    buildOutput, err := contain.RunAppend(cfg, layers)
    Expect(err).NotTo(HaveOccurred())
    Expect(buildOutput).NotTo(BeNil())
    Expect(buildOutput.Skaffold).NotTo(BeNil())
    Expect(len(buildOutput.Skaffold.Builds)).To(Equal(1))

    artifact := buildOutput.Skaffold.Builds[0]
    desc, err := remote.Get(artifact.Reference(), testCraneOptions.Remote...)
    Expect(err).NotTo(HaveOccurred())
    raw, err := desc.RawManifest()
    Expect(err).NotTo(HaveOccurred())

    // Parse manifest to examine annotations
    var manifest map[string]interface{}
    Expect(json.Unmarshal(raw, &manifest)).To(Succeed())
    anns, ok := manifest["annotations"].(map[string]interface{})
    if !ok {
        t.Fatalf("annotations missing: %v", manifest)
    }
    var expectedBaseName string
    if at := strings.Index(cfg.Base, "@"); at > 0 {
        expectedBaseName = cfg.Base[:at]
        if o := os.Getenv("CONTAIN_ANNOTATIONS_BASE_REGISTRY_HOST_OVERRIDE"); o != "" {
            if slash := strings.Index(expectedBaseName, "/"); slash > 0 {
                expectedBaseName = o + expectedBaseName[slash:]
            } else {
                expectedBaseName = o
            }
        }
    }
    var expectedBaseDigest string
    if at := strings.Index(cfg.Base, "@"); at > 0 {
        expectedBaseDigest = cfg.Base[at+1:]
    }
    Expect(anns["org.opencontainers.image.base.digest"]).To(Equal(expectedBaseDigest))
    Expect(anns["org.opencontainers.image.base.name"]).To(Equal(expectedBaseName))
}

// TestBaseImageAnnotationsWithoutOverride verifies annotations when override env is NOT set.
func TestBaseImageAnnotationsWithoutOverride(t *testing.T) {
    RegisterTestingT(t)
    // Save and unset override
    original := os.Getenv("CONTAIN_ANNOTATIONS_BASE_REGISTRY_HOST_OVERRIDE")
    _ = os.Unsetenv("CONTAIN_ANNOTATIONS_BASE_REGISTRY_HOST_OVERRIDE")
    defer func() {
        if original != "" { os.Setenv("CONTAIN_ANNOTATIONS_BASE_REGISTRY_HOST_OVERRIDE", original) }
    }()

    dir := testcases.NewTempDir(t)
    dir.Write("annotate.txt", "data2")

    cfg := schema.ContainConfig{
        Base: "contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4",
        Tag:  "contain-test/annotate:nooverride",
        Layers: []schema.Layer{{LocalDir: schema.LocalDir{Path: ".", ContainerPath: "/app"}}},
        Platforms: []string{"linux/amd64"},
    }
    cfg.Base = fmt.Sprintf("%s/%s", testRegistry, cfg.Base)
    cfg.Tag = fmt.Sprintf("%s/%s", testRegistry, cfg.Tag)

    layers, err := contain.RunLayers(cfg)
    Expect(err).NotTo(HaveOccurred())
    buildOutput, err := contain.RunAppend(cfg, layers)
    Expect(err).NotTo(HaveOccurred())
    Expect(buildOutput).NotTo(BeNil())
    Expect(buildOutput.Skaffold).NotTo(BeNil())
    Expect(len(buildOutput.Skaffold.Builds)).To(Equal(1))

    artifact := buildOutput.Skaffold.Builds[0]
    desc, err := remote.Get(artifact.Reference(), testCraneOptions.Remote...)
    Expect(err).NotTo(HaveOccurred())
    raw, err := desc.RawManifest()
    Expect(err).NotTo(HaveOccurred())

    var manifest map[string]interface{}
    Expect(json.Unmarshal(raw, &manifest)).To(Succeed())
    anns, ok := manifest["annotations"].(map[string]interface{})
    if !ok { t.Fatalf("annotations missing: %v", manifest) }

    var expectedBaseName string
    if at := strings.Index(cfg.Base, "@"); at > 0 { expectedBaseName = cfg.Base[:at] }
    var expectedBaseDigest string
    if at := strings.Index(cfg.Base, "@"); at > 0 { expectedBaseDigest = cfg.Base[at+1:] }

    Expect(anns["org.opencontainers.image.base.digest"]).To(Equal(expectedBaseDigest))
    Expect(anns["org.opencontainers.image.base.name"]).To(Equal(expectedBaseName))
}

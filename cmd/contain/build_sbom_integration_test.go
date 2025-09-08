package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "reflect"
    "testing"

    "github.com/turbokube/contain/pkg/sbom"
    "github.com/turbokube/contain/pkg/testcases"
)

// Test that `contain build --sbom-in --sbom-out` yields the same SPDX JSON
// as running `contain build` followed by `contain sbom --build-artifacts ... --in ... --out ...`.
func TestBuildWithSBOMFlags_EqualsSeparateSbom(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    r := testcases.NewTestregistry(ctx)
    if err := r.Start(); err != nil {
        t.Fatalf("start registry: %v", err)
    }

    // Temp workspace
    dir := t.TempDir()
    // Minimal context content
    if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("hello"), 0o644); err != nil {
        t.Fatal(err)
    }

    // Config: base is the prepopulated test base; tag points to our ephemeral registry
    baseRef := fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest@sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4", r.Host)
    tagRef := fmt.Sprintf("%s/contain-test/sbom:it", r.Host)
    cfg := []byte(fmt.Sprintf(
        "base: %s\n"+
            "tag: %s\n"+
            "layers:\n"+
            "- localDir:\n"+
            "    path: .\n"+
            "    containerPath: /app\n",
        baseRef, tagRef,
    ))
    cfgPath := filepath.Join(dir, "contain.yaml")
    if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
        t.Fatal(err)
    }

    // SPDX input fixture
    inSPDX := filepath.Join(dir, "in.spdx.json")
    data, err := os.ReadFile(filepath.Join("../../pkg/sbom/testdata", "nodejs1.spdx.json"))
    if err != nil {
        t.Fatalf("read testdata: %v", err)
    }
    if err := os.WriteFile(inSPDX, data, 0o644); err != nil {
        t.Fatal(err)
    }

    // Provide deterministic base for both flows; this overrides any remote discovery.
    os.Setenv("CONTAIN_SPDX_BASE_NAME", fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest", r.Host))
    os.Setenv("CONTAIN_SPDX_BASE_DIGEST", "sha256:f9f2106a04a339d282f1152f0be7c9ce921a0c01320de838cda364948de66bd4")
    t.Cleanup(func() {
        os.Unsetenv("CONTAIN_SPDX_BASE_NAME")
        os.Unsetenv("CONTAIN_SPDX_BASE_DIGEST")
    })

    // Paths for outputs
    buildsJSON := filepath.Join(dir, "builds.json")
    combinedOut := filepath.Join(dir, "combined.spdx.json")
    separateOut := filepath.Join(dir, "separate.spdx.json")

    // --- Combined flow: contain build --file-output builds.json --sbom-in in --sbom-out combined
    // set global flags used by runBuild
    configPath = cfgPath
    base = "" // keep from config
    runSelector = ""
    runNamespace = ""
    watch = false
    fileOutput = buildsJSON
    metadataFile = ""
    platformsEnv = false
    sbomInFile = inSPDX
    sbomOutFile = combinedOut

    if err := runBuild([]string{dir}); err != nil {
        t.Fatalf("runBuild combined: %v", err)
    }

    // --- Separate flow: contain sbom --build-artifacts builds.json --in in --out separate
    if err := sbom.WrapSPDX(buildsJSON, inSPDX, separateOut, nil, BUILD); err != nil {
        t.Fatalf("wrap separate: %v", err)
    }

    // Read and compare JSON (structural equality)
    cmp := func(path string) map[string]interface{} {
        raw, err := os.ReadFile(path)
        if err != nil {
            t.Fatalf("read %s: %v", path, err)
        }
        var m map[string]interface{}
        if err := json.Unmarshal(raw, &m); err != nil {
            t.Fatalf("unmarshal %s: %v", path, err)
        }
        return m
    }
    a := cmp(combinedOut)
    b := cmp(separateOut)
    if !reflect.DeepEqual(a, b) {
        t.Fatalf("combined vs separate SPDX differ")
    }
}

package spdx_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	v1hash "github.com/google/go-containerregistry/pkg/v1"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/contain"
	v1 "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/spdx"
)

// example SPDX document copied (with dependencies + relationships) from test/esbuild-main/target/spdx.json
const exampleSPDX = `{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "contain-test-esbuild-main@1.0.0",
  "documentNamespace": "http://spdx.org/spdxdocs/contain-test-esbuild-main-1.0.0-1f1d0fe0-6f3e-41f6-b93b-b92f5c01bf10",
  "creationInfo": {
    "created": "2025-08-18T10:59:01.960Z",
    "creators": [
      "Tool: npm/cli-10.9.2"
    ]
  },
  "documentDescribes": [
    "SPDXRef-Package-contain-test-esbuild-main-1.0.0"
  ],
  "packages": [
    {
      "name": "contain-test-esbuild-main",
      "SPDXID": "SPDXRef-Package-contain-test-esbuild-main-1.0.0",
      "versionInfo": "1.0.0",
      "packageFileName": "",
      "primaryPackagePurpose": "APPLICATION",
      "downloadLocation": "NOASSERTION",
      "filesAnalyzed": false,
      "homepage": "NOASSERTION",
      "licenseDeclared": "UNLICENSED",
      "externalRefs": [
        {
          "referenceCategory": "PACKAGE-MANAGER",
          "referenceType": "purl",
          "referenceLocator": "pkg:npm/contain-test-esbuild-main@1.0.0"
        }
      ]
    },
    {
      "name": "hono",
      "SPDXID": "SPDXRef-Package-hono-4.9.2",
      "versionInfo": "4.9.2",
      "packageFileName": "node_modules/hono",
      "downloadLocation": "https://registry.npmjs.org/hono/-/hono-4.9.2.tgz",
      "filesAnalyzed": false,
      "homepage": "NOASSERTION",
      "licenseDeclared": "MIT",
      "externalRefs": [
        {
          "referenceCategory": "PACKAGE-MANAGER",
          "referenceType": "purl",
          "referenceLocator": "pkg:npm/hono@4.9.2"
        }
      ],
      "checksums": [
        {
          "algorithm": "SHA512",
          "checksumValue": "506da35c64bf8242c7e3697fd6e44e9f05e9923befc64977929a292f72c1a36ecdb9a0cf23ac4735fb944a29481dcac190f7e5e32d33eb2d81c88e39e538cd47"
        }
      ]
    }
  ],
  "relationships": [
    {
      "spdxElementId": "SPDXRef-DOCUMENT",
      "relatedSpdxElement": "SPDXRef-Package-contain-test-esbuild-main-1.0.0",
      "relationshipType": "DESCRIBES"
    },
    {
      "spdxElementId": "SPDXRef-Package-hono-4.9.2",
      "relatedSpdxElement": "SPDXRef-Package-contain-test-esbuild-main-1.0.0",
      "relationshipType": "DEPENDENCY_OF"
    }
  ]
}`

// minimal structure for assertions
type pkg struct {
	Name    string `json:"name"`
	Purpose string `json:"primaryPackagePurpose"`
}
type relationship struct {
	Type string `json:"relationshipType"`
}
type doc struct {
	Packages      []pkg          `json:"packages"`
	Relationships []relationship `json:"relationships"`
}

func TestAppendToAddsContainerImages(t *testing.T) {
	g := NewWithT(t)

	tmp := t.TempDir()
	spdxFile := filepath.Join(tmp, "spdx.json")
	g.Expect(os.WriteFile(spdxFile, []byte(exampleSPDX), 0o644)).To(Succeed())

	// stub build output with digest
	digest := v1hash.Hash{Algorithm: "sha256", Hex: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	buildOutput, err := contain.NewBuildOutput("example.net/misc/result-image:cde", digest)
	g.Expect(err).NotTo(HaveOccurred())

	cfg := v1.ContainConfig{Base: "example.net/misc/base-image:abc@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}

	err = spdx.AppendTo(spdxFile, cfg, buildOutput)
	g.Expect(err).NotTo(HaveOccurred())

	raw, err := os.ReadFile(spdxFile)
	g.Expect(err).NotTo(HaveOccurred())

	var d doc
	g.Expect(json.Unmarshal(raw, &d)).To(Succeed())

	// collect container packages
	var containers []string
	for _, p := range d.Packages {
		if p.Purpose == "CONTAINER" {
			containers = append(containers, p.Name)
		}
	}
	g.Expect(containers).To(HaveLen(2), "should have two CONTAINER packages (base + result)")

	// ensure original relationships survived
	var hasDependency bool
	for _, r := range d.Relationships {
		if r.Type == "DEPENDENCY_OF" {
			hasDependency = true
		}
	}
	g.Expect(hasDependency).To(BeTrue(), "existing relationships must be preserved")

	// check base exact and result pattern
	var baseFound, resultFound bool
	reResult := regexp.MustCompile(`^example\.net/misc/result-image:cde@sha256:[0-9a-f]{64}$`)
	for _, name := range containers {
		if name == "example.net/misc/base-image:abc" {
			baseFound = true
		}
		if reResult.MatchString(name) {
			resultFound = true
		}
	}
	g.Expect(baseFound).To(BeTrue(), "base image container package missing")
	g.Expect(resultFound).To(BeTrue(), "result image container package with digest missing")
}

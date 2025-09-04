package sbom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapSPDX_AppendsCreatorAndEnrichment(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	out := filepath.Join(dir, "out.json")

	// Base fixture SPDX
	data, err := os.ReadFile(filepath.Join("testdata", "nodejs1.spdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate build artifacts (skaffold superset) with a result image digest
	builds := `{"builds":[{"imageName":"example.net/misc/result-image:cde","tagRef":"example.net/misc/result-image:cde@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]}`
	ba := filepath.Join(dir, "builds.json")
	if err := os.WriteFile(ba, []byte(builds), 0o644); err != nil {
		t.Fatal(err)
	}

	// Provide base via env override to avoid remote calls
	os.Setenv("CONTAIN_SPDX_BASE_NAME", "example.net/misc/base-image:abc")
	os.Setenv("CONTAIN_SPDX_BASE_DIGEST", "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	t.Cleanup(func() { os.Unsetenv("CONTAIN_SPDX_BASE_NAME"); os.Unsetenv("CONTAIN_SPDX_BASE_DIGEST") })

	version := "testversion"
	if err := WrapSPDX(ba, in, out, nil, version); err != nil {
		t.Fatalf("WrapSPDX error: %v", err)
	}

	// Read back JSON
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	// creators includes Tool: contain-<version>
	ci := doc["creationInfo"].(map[string]interface{})
	creators := ci["creators"].([]interface{})
	found := false
	for _, v := range creators {
		if v.(string) == "Tool: contain-"+version {
			found = true
		}
	}
	if !found {
		t.Fatalf("creator not appended")
	}

	// container packages: base (with checksum) and result (name includes @sha256:)
	pkgs := doc["packages"].([]interface{})
	var baseOK, resultOK bool
	for _, v := range pkgs {
		m := v.(map[string]interface{})
		if m["primaryPackagePurpose"] == "CONTAINER" {
			name := m["name"].(string)
			if name == "example.net/misc/base-image:abc" {
				// has SHA256 checksum
				checks, _ := m["checksums"].([]interface{})
				for _, c := range checks {
					if cm, ok := c.(map[string]interface{}); ok && cm["algorithm"] == "SHA256" {
						baseOK = true
					}
				}
			}
			if strings.HasPrefix(name, "example.net/misc/result-image:cde@sha256:") && len(name) == len("example.net/misc/result-image:cde@sha256:")+64 {
				resultOK = true
			}
		}
	}
	if !baseOK {
		t.Fatalf("base container package with checksum missing")
	}
	if !resultOK {
		t.Fatalf("result container package with digest missing")
	}

	// documentDescribes includes result package SPDXID
	var resultID string
	for _, v := range pkgs {
		m := v.(map[string]interface{})
		if strings.HasPrefix(m["name"].(string), "example.net/misc/result-image:cde@sha256:") {
			resultID = m["SPDXID"].(string)
		}
	}
	dd := doc["documentDescribes"].([]interface{})
	hasDD := false
	for _, v := range dd {
		if v.(string) == resultID {
			hasDD = true
		}
	}
	if !hasDD {
		t.Fatalf("documentDescribes missing result")
	}

	// Relationship: RESULT DESCENDANT_OF BASE and RESULT DEPENDS_ON BASE
	var baseID string
	for _, v := range pkgs {
		m := v.(map[string]interface{})
		if m["name"].(string) == "example.net/misc/base-image:abc" {
			baseID = m["SPDXID"].(string)
		}
	}
	rels := doc["relationships"].([]interface{})
	var hasDescendant bool
	var hasDepends bool
	var appDependsStill bool
	var appID string
	// find incoming app package SPDXID
	for _, v := range pkgs {
		m := v.(map[string]interface{})
		if m["primaryPackagePurpose"] == "APPLICATION" {
			appID = m["SPDXID"].(string)
		}
	}
	for _, v := range rels {
		m := v.(map[string]interface{})
		if m["relationshipType"] == "DESCENDANT_OF" && m["spdxElementId"] == resultID && m["relatedSpdxElement"] == baseID {
			hasDescendant = true
		}
		if m["relationshipType"] == "DEPENDS_ON" && m["spdxElementId"] == resultID && m["relatedSpdxElement"] == baseID {
			hasDepends = true
		}
		if m["relationshipType"] == "DEPENDENCY_OF" && m["spdxElementId"] == appID && m["relatedSpdxElement"] == resultID {
			appDependsStill = true
		}
	}
	if !hasDescendant {
		t.Fatalf("DESCENDANT_OF relationship missing")
	}
	if !hasDepends {
		t.Fatalf("DEPENDS_ON relationship missing")
	}
	if appID != "" && !appDependsStill {
		t.Fatalf("incoming app dependency on result missing")
	}
}

package sbom

import (
	"os"
	"path/filepath"
	"testing"

	spdxjson "github.com/spdx/tools-golang/json"
)

func TestWrapSPDX_AppendsCreator(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	out := filepath.Join(dir, "out.json")

	// Use the provided fixture content
	data, err := os.ReadFile(filepath.Join("testdata", "nodejs1.spdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}

	version := "testversion"
	if err := WrapSPDX("", in, out, nil, version); err != nil {
		t.Fatalf("WrapSPDX error: %v", err)
	}

	// Read back and assert creator appended
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := spdxjson.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	want := toolName + "-" + version
	found := false
	if doc.CreationInfo != nil {
		for _, c := range doc.CreationInfo.Creators {
			if c.Creator == want && c.CreatorType == "Tool" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected creator %q not found", want)
	}
}

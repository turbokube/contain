package spdx

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/turbokube/contain/pkg/contain"
	schemav1 "github.com/turbokube/contain/pkg/schema/v1"
)

func AppendTo(spdxFile string, config schemav1.ContainConfig, buildOutput *contain.BuildOutput) error {
	if config.Base == "" {
		return fmt.Errorf("base image must be set to append to spdx file")
	}
	if buildOutput == nil || buildOutput.Skaffold == nil || len(buildOutput.Skaffold.Builds) == 0 {
		return fmt.Errorf("build output with at least one artifact required")
	}

	f, err := os.Open(spdxFile)
	if err != nil {
		return fmt.Errorf("open spdx: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("read spdx: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return fmt.Errorf("unmarshal spdx: %w", err)
	}

	// get packages slice (may be absent)
	pkgsAny, ok := root["packages"].([]any)
	if !ok {
		pkgsAny = []any{}
	}

	hasContainerName := func(name string) bool {
		for _, v := range pkgsAny {
			if m, ok := v.(map[string]any); ok {
				if purpose, _ := m["primaryPackagePurpose"].(string); purpose == "CONTAINER" {
					if n, _ := m["name"].(string); n == name { return true }
				}
			}
		}
		return false
	}

	baseName := config.Base
	if i := strings.Index(baseName, "@sha256:"); i != -1 { baseName = baseName[:i] }
	if !hasContainerName(baseName) {
		pkgsAny = append(pkgsAny, map[string]any{
			"name": baseName,
			"SPDXID": makePackageID(baseName),
			"primaryPackagePurpose": "CONTAINER",
			"downloadLocation": "NOASSERTION",
			"filesAnalyzed": false,
			"homepage": "NOASSERTION",
			"licenseDeclared": "NOASSERTION",
		})
	}

	result := buildOutput.Skaffold.Builds[0]
	resultName := result.Tag
	if !strings.Contains(resultName, "@sha256:") {
		h := result.Http().Hash
		if h.Algorithm == "sha256" && h.Hex != "" {
			resultName = fmt.Sprintf("%s@%s:%s", result.ImageName, h.Algorithm, h.Hex)
		}
	}
	if !hasContainerName(resultName) {
		pkgsAny = append(pkgsAny, map[string]any{
			"name": resultName,
			"SPDXID": makePackageID(resultName),
			"primaryPackagePurpose": "CONTAINER",
			"downloadLocation": "NOASSERTION",
			"filesAnalyzed": false,
			"homepage": "NOASSERTION",
			"licenseDeclared": "NOASSERTION",
		})
	}

	// reorder: keep non-container first in original order, then sort containers by name for determinism
	var non, containers []map[string]any
	for _, v := range pkgsAny {
		m := v.(map[string]any)
		if purpose, _ := m["primaryPackagePurpose"].(string); purpose == "CONTAINER" {
			containers = append(containers, m)
		} else {
			non = append(non, m)
		}
	}
	sort.Slice(containers, func(i, j int) bool {
		ni, _ := containers[i]["name"].(string)
		nj, _ := containers[j]["name"].(string)
		return ni < nj
	})
	merged := make([]any, 0, len(pkgsAny))
	for _, m := range non { merged = append(merged, m) }
	for _, m := range containers { merged = append(merged, m) }
	root["packages"] = merged

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil { return fmt.Errorf("marshal spdx: %w", err) }
	if err := os.WriteFile(spdxFile, out, 0o644); err != nil {
		return fmt.Errorf("write spdx: %w", err)
	}
	return nil
}

var nonAlphaNum = regexp.MustCompile(`[^A-Za-z0-9]+`)

func makePackageID(name string) string {
	cleaned := strings.Trim(nonAlphaNum.ReplaceAllString(name, "-"), "-")
	if cleaned == "" {
		cleaned = "image"
	}
	return "SPDXRef-Package-" + cleaned
}

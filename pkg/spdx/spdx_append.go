package spdx

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	spdxjson "github.com/spdx/tools-golang/json"
	"github.com/spdx/tools-golang/spdx"
	common "github.com/spdx/tools-golang/spdx/v2/common"

	"github.com/turbokube/contain/pkg/contain"
	schemav1 "github.com/turbokube/contain/pkg/schema/v1"
)

const toolName = "contain"

// AppendTo loads an SPDX JSON document, appends container image packages (base + build result),
// ensures checksum for base digest, adds a creator Tool entry, and writes back JSON.
func AppendTo(spdxFile string, config schemav1.ContainConfig, buildOutput *contain.BuildOutput, version string) error {
	if config.Base == "" {
		return fmt.Errorf("base image must be set to append to spdx file")
	}
	if buildOutput == nil || buildOutput.Skaffold == nil || len(buildOutput.Skaffold.Builds) == 0 {
		return fmt.Errorf("build output with at least one artifact required")
	}

	// read existing doc
	f, err := os.Open(spdxFile)
	if err != nil {
		return fmt.Errorf("open spdx: %w", err)
	}
	defer f.Close()
	doc, err := spdxjson.Read(f)
	if err != nil {
		return fmt.Errorf("read spdx: %w", err)
	}

	// Build index of existing container package names
	hasContainer := func(name string) bool {
		for _, p := range doc.Packages {
			if p != nil && p.PrimaryPackagePurpose == "CONTAINER" && p.PackageName == name {
				return true
			}
		}
		return false
	}

	baseNameFull := config.Base
	baseName := baseNameFull
	var baseDigest string
	if i := strings.Index(baseNameFull, "@sha256:"); i != -1 {
		baseName = baseNameFull[:i]
		baseDigest = baseNameFull[i+len("@sha256:"):]
	}

	// helper to make checksum list
	makeChecksums := func(d string) []common.Checksum {
		if len(d) == 64 { // hex length
			return []common.Checksum{{Algorithm: common.SHA256, Value: d}}
		}
		return nil
	}

	// Add base package if missing
	if !hasContainer(baseName) {
		pkg := &spdx.Package{
			PackageName:             baseName,
			PackageSPDXIdentifier:   common.ElementID(makePackageID(baseName)),
			PrimaryPackagePurpose:   "CONTAINER",
			PackageDownloadLocation: "NOASSERTION",
			FilesAnalyzed:           false,
			PackageHomePage:         "NOASSERTION",
			PackageLicenseDeclared:  "NOASSERTION",
			PackageChecksums:        makeChecksums(baseDigest),
		}
		doc.Packages = append(doc.Packages, pkg)
	} else if baseDigest != "" { // ensure checksum present on existing
		for _, p := range doc.Packages {
			if p.PackageName == baseName {
				if p.PackageChecksums == nil || len(p.PackageChecksums) == 0 {
					p.PackageChecksums = makeChecksums(baseDigest)
				}
			}
		}
	}

	// result image name (include digest if not already)
	result := buildOutput.Skaffold.Builds[0]
	resultName := result.Tag
	if !strings.Contains(resultName, "@sha256:") {
		h := result.Http().Hash
		if h.Algorithm == "sha256" && h.Hex != "" {
			resultName = fmt.Sprintf("%s@%s:%s", result.ImageName, h.Algorithm, h.Hex)
		}
	}
	if !hasContainer(resultName) {
		pkg := &spdx.Package{
			PackageName:             resultName,
			PackageSPDXIdentifier:   common.ElementID(makePackageID(resultName)),
			PrimaryPackagePurpose:   "CONTAINER",
			PackageDownloadLocation: "NOASSERTION",
			FilesAnalyzed:           false,
			PackageHomePage:         "NOASSERTION",
			PackageLicenseDeclared:  "NOASSERTION",
		}
		doc.Packages = append(doc.Packages, pkg)
	}

	// Reorder: non-container keep order, containers sorted by name
	var non, containers []*spdx.Package
	for _, p := range doc.Packages {
		if p == nil || p.PrimaryPackagePurpose != "CONTAINER" {
			non = append(non, p)
		} else {
			containers = append(containers, p)
		}
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].PackageName < containers[j].PackageName })
	doc.Packages = append(append([]*spdx.Package{}, non...), containers...)

	// Ensure creation info & creators
	if doc.CreationInfo == nil {
		doc.CreationInfo = &spdx.CreationInfo{}
	}
	toolValue := fmt.Sprintf("%s-%s", toolName, version)
	present := false
	for _, c := range doc.CreationInfo.Creators {
		if c.CreatorType == "Tool" && c.Creator == toolValue {
			present = true
			break
		}
	}
	if !present {
		doc.CreationInfo.Creators = append(doc.CreationInfo.Creators, common.Creator{CreatorType: "Tool", Creator: toolValue})
	}

	// write back
	// write back
	out, err := os.Create(spdxFile)
	if err != nil {
		return fmt.Errorf("create spdx: %w", err)
	}
	defer out.Close()
	if err := spdxjson.Write(doc, out); err != nil {
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

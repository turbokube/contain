package sbom

import (
	"fmt"
	"os"

	spdxjson "github.com/spdx/tools-golang/json"
	"github.com/spdx/tools-golang/spdx"
	common "github.com/spdx/tools-golang/spdx/v2/common"

	"github.com/turbokube/contain/pkg/pushed"
)

const toolName = "contain"

// WrapSPDX reads an SPDX JSON document, appends a creator entry for contain tool version,
// and writes the resulting document to outFile (or back to inFile if outFile is empty).
// Future: buildArtifactsPath and artifact may be used to enrich the document; unused for now.
func WrapSPDX(buildArtifactsPath string, inFile string, outFile string, artifact *pushed.Artifact, toolVersion string) error { //nolint:revive,unused
	// Read existing document
	f, err := os.Open(inFile)
	if err != nil {
		return fmt.Errorf("open spdx: %w", err)
	}
	defer f.Close()
	doc, err := spdxjson.Read(f)
	if err != nil {
		return fmt.Errorf("read spdx: %w", err)
	}

	// Ensure creation info and add tool creator if missing
	if doc.CreationInfo == nil {
		doc.CreationInfo = &spdx.CreationInfo{}
	}
	toolValue := fmt.Sprintf("%s-%s", toolName, toolVersion)
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

	// Write back
	target := outFile
	if target == "" {
		target = inFile
	}
	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create spdx: %w", err)
	}
	defer out.Close()
	if err := spdxjson.Write(doc, out, spdxjson.Indent("  ")); err != nil {
		return fmt.Errorf("write spdx: %w", err)
	}
	return nil
}

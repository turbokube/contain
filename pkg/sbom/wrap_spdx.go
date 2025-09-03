package sbom

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/turbokube/contain/pkg/pushed"
)

const toolName = "contain"

// WrapSPDX reads an SPDX JSON document, appends a creator entry for contain tool version,
// and writes the resulting document to outFile (or back to inFile if outFile is empty).
// Future: buildArtifactsPath and artifact may be used to enrich the document; unused for now.
func WrapSPDX(buildArtifactsPath string, inFile string, outFile string, artifact *pushed.Artifact, toolVersion string) error { //nolint:revive,unused
	// Read existing document JSON
	raw, err := os.ReadFile(inFile)
	if err != nil {
		return fmt.Errorf("open spdx: %w", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("read spdx: %w", err)
	}
	ensureCreator(doc, toolVersion)

	// Discover build result (image) from build artifacts JSON or provided artifact
	var resImageName string
	var resDigest v1.Hash
	var tagRef string
	if artifact != nil && artifact.TagRef != "" {
		resImageName = artifact.ImageName
		tagRef = artifact.TagRef
	}
	if tagRef == "" && buildArtifactsPath != "" { // try skaffold-style
		var s struct {
			Builds []struct{ ImageName, TagRef, Tag string } `json:"builds"`
		}
		if b, err := os.ReadFile(buildArtifactsPath); err == nil {
			_ = json.Unmarshal(b, &s)
			if len(s.Builds) > 0 {
				resImageName = s.Builds[0].ImageName
				if s.Builds[0].TagRef != "" {
					tagRef = s.Builds[0].TagRef
				} else {
					tagRef = s.Builds[0].Tag
				}
			}
		}
	}
	// Parse digest from tagRef (name@sha256:hex)
	if tagRef != "" {
		if at := strings.LastIndex(tagRef, "@"); at != -1 {
			if h, err := v1.NewHash(tagRef[at+1:]); err == nil {
				resDigest = h
			}
		}
	}

	// If we found a result image, add container package for it and make it top-level deliverable
	var resultID string
	if resImageName != "" && resDigest.Algorithm == "sha256" && resDigest.Hex != "" {
		resultPkgName := fmt.Sprintf("%s@%s:%s", resImageName, resDigest.Algorithm, resDigest.Hex)
		resultID = ensureContainerPackageJSON(&doc, resultPkgName, "")
		ensureDocumentDescribesJSON(&doc, resultID)
	}

	// Attempt to discover base image from annotations of the built image (best-effort)
	var baseName string
	var baseDigestHex string
	if tagRef != "" {
		baseName, baseDigestHex = discoverBaseFromImage(tagRef)
	}
	// Optional env override for tests or CI
	if bn := os.Getenv("CONTAIN_SPDX_BASE_NAME"); bn != "" {
		baseName = bn
	}
	if bd := os.Getenv("CONTAIN_SPDX_BASE_DIGEST"); bd != "" {
		baseDigestHex = trimSha256Prefix(bd)
	}

	var baseID string
	if baseName != "" {
		baseID = ensureContainerPackageJSON(&doc, baseName, baseDigestHex)
	}
	// Relationship: result DESCENDANT_OF base (if both present)
	if resultID != "" && baseID != "" {
		ensureRelationshipJSON(&doc, resultID, baseID, "DESCENDANT_OF")
	}

	// Write back
	target := outFile
	if target == "" {
		target = inFile
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal spdx: %w", err)
	}
	if err := os.WriteFile(target, b, 0o644); err != nil {
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

func trimSha256Prefix(s string) string {
	if strings.HasPrefix(s, "sha256:") {
		return s[len("sha256:"):]
	}
	return s
}

// --- JSON helpers ---

func ensureCreator(doc map[string]interface{}, version string) {
	ci := ensureMap(doc, "creationInfo")
	creators := ensureSlice(ci, "creators")
	want := "Tool: " + toolName + "-" + version
	for _, v := range creators {
		if s, ok := v.(string); ok && s == want {
			return
		}
	}
	ci["creators"] = append(creators, want)
}

func ensureContainerPackageJSON(doc *map[string]interface{}, name string, checksumHex string) string {
	d := *doc
	pkgName := strings.TrimPrefix(name, "/")
	pkgs := ensureSlice(d, "packages")
	// find existing
	for i := range pkgs {
		if m, ok := pkgs[i].(map[string]interface{}); ok {
			if m["primaryPackagePurpose"] == "CONTAINER" && m["name"] == pkgName {
				// ensure checksum
				if checksumHex != "" {
					ensureChecksum(m, checksumHex)
				}
				if id, _ := m["SPDXID"].(string); id != "" {
					return id
				}
				id := makePackageID(pkgName)
				m["SPDXID"] = id
				return id
			}
		}
	}
	// create new
	id := makePackageID(pkgName)
	m := map[string]interface{}{
		"name":                  pkgName,
		"SPDXID":                id,
		"primaryPackagePurpose": "CONTAINER",
		"downloadLocation":      "NOASSERTION",
		"filesAnalyzed":         false,
		"homepage":              "NOASSERTION",
		"licenseDeclared":       "NOASSERTION",
	}
	if checksumHex != "" {
		ensureChecksum(m, checksumHex)
	}
	d["packages"] = append(pkgs, m)
	return id
}

func ensureChecksum(pkg map[string]interface{}, hex string) {
	checks, _ := pkg["checksums"].([]interface{})
	// if already has SHA256, don't duplicate
	for _, c := range checks {
		if cm, ok := c.(map[string]interface{}); ok {
			if cm["algorithm"] == "SHA256" {
				return
			}
		}
	}
	pkg["checksums"] = append(checks, map[string]interface{}{"algorithm": "SHA256", "checksumValue": hex})
}

func ensureDocumentDescribesJSON(doc *map[string]interface{}, id string) {
	d := *doc
	lst := ensureSlice(d, "documentDescribes")
	for _, v := range lst {
		if s, ok := v.(string); ok && s == id {
			return
		}
	}
	d["documentDescribes"] = append(lst, id)
}

func ensureRelationshipJSON(doc *map[string]interface{}, fromID, toID, relType string) {
	d := *doc
	rels := ensureSlice(d, "relationships")
	for _, v := range rels {
		if m, ok := v.(map[string]interface{}); ok {
			if m["spdxElementId"] == fromID && m["relatedSpdxElement"] == toID && m["relationshipType"] == relType {
				return
			}
		}
	}
	d["relationships"] = append(rels, map[string]interface{}{
		"spdxElementId":      fromID,
		"relatedSpdxElement": toID,
		"relationshipType":   relType,
	})
}

func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	if v, ok := parent[key].(map[string]interface{}); ok {
		return v
	}
	m := map[string]interface{}{}
	parent[key] = m
	return m
}

func ensureSlice(parent map[string]interface{}, key string) []interface{} {
	if v, ok := parent[key].([]interface{}); ok {
		return v
	}
	s := []interface{}{}
	parent[key] = s
	return s
}

// discoverBaseFromImage tries to fetch the image (or index) and read base annotations from image manifests.
// Returns base name (without digest) and digest hex if found. Best-effort; returns empty strings if not found.
func discoverBaseFromImage(tagRef string) (string, string) {
	// Parse as digest reference; prefer insecure for localhost/127.0.0.1
	parse := func(insecure bool) (name.Digest, error) {
		if insecure {
			return name.NewDigest(tagRef, name.Insecure)
		}
		return name.NewDigest(tagRef)
	}
	d, err := parse(false)
	if err != nil {
		// Try insecure if localhost or 127.0.0.1
		host := hostFromRef(tagRef)
		if isLocalhost(host) {
			if di, err2 := parse(true); err2 == nil {
				d = di
			} else {
				return "", ""
			}
		} else {
			return "", ""
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Determine if index or image by HEAD descriptor
	desc, err := remote.Head(d, remote.WithContext(ctx))
	if err != nil {
		return "", ""
	}
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		idx, err := remote.Index(d, remote.WithContext(ctx))
		if err != nil {
			return "", ""
		}
		im, err := idx.IndexManifest()
		if err != nil {
			return "", ""
		}
		for _, m := range im.Manifests {
			if m.MediaType == types.OCIManifestSchema1 || m.MediaType == types.DockerManifestSchema2 {
				// build a digest reference within same repository
				childRefStr := d.Context().Name() + "@" + m.Digest.String()
				childRef, err := name.NewDigest(childRefStr)
				if err != nil {
					continue
				}
				if isLocalhost(d.Context().RegistryStr()) {
					childRef, _ = name.NewDigest(childRefStr, name.Insecure)
				}
				img, err := remote.Image(childRef, remote.WithContext(ctx))
				if err != nil {
					continue
				}
				man, err := img.Manifest()
				if err != nil || man == nil {
					continue
				}
				if name, dig := baseFromAnnotations(man.Annotations); name != "" || dig != "" {
					return name, dig
				}
			}
		}
	case types.OCIManifestSchema1, types.DockerManifestSchema2:
		img, err := remote.Image(d, remote.WithContext(ctx))
		if err != nil {
			return "", ""
		}
		man, err := img.Manifest()
		if err != nil || man == nil {
			return "", ""
		}
		return baseFromAnnotations(man.Annotations)
	}
	return "", ""
}

func baseFromAnnotations(a map[string]string) (string, string) {
	if a == nil {
		return "", ""
	}
	name := strings.TrimPrefix(a[specsv1.AnnotationBaseImageName], "/")
	digest := trimSha256Prefix(a[specsv1.AnnotationBaseImageDigest])
	return name, digest
}

func hostFromRef(ref string) string {
	if i := strings.Index(ref, "/"); i > 0 {
		return ref[:i]
	}
	return ref
}

func isLocalhost(host string) bool {
	if host == "localhost" || strings.HasPrefix(host, "localhost:") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	if i := strings.Index(host, ":"); i != -1 {
		if ip := net.ParseIP(host[:i]); ip != nil {
			return ip.IsLoopback()
		}
	}
	return false
}

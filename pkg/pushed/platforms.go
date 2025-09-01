package pushed

import (
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func toPlatforms(platforms []v1.Platform) []string {
	if len(platforms) == 0 {
		return nil // works with omitempty
	}
	result := make([]string, 0, len(platforms))
	for _, pf := range platforms {
		result = append(result, pf.String())
	}
	return result
}

// platformsFromIndexManifest returns a slice of platforms present in the index manifest.
// It includes only image manifests with a defined platform, skips attestations and non-image entries.
func platformsFromIndexManifest(idxm *v1.IndexManifest) []v1.Platform {
	platforms := make([]v1.Platform, 0, len(idxm.Manifests))
	for _, d := range idxm.Manifests {
		// Only include image manifests with a platform
		if d.Platform == nil {
			continue
		}
		if d.MediaType != types.OCIManifestSchema1 {
			// Skip non-image manifest entries (e.g., referrers/other types)
			continue
		}
		if d.Annotations != nil {
			if d.Platform.String() == AttestationPlatform && d.Annotations[ReferenceTypeAnnotation] == ReferenceTypeAttestation {
				// Skip attestation manifests
				continue
			}
		}
		platforms = append(platforms, *d.Platform)
	}
	return platforms
}

// platformsFromStrings parses platform strings like "linux/amd64" or "linux/arm64/v8" into v1.Platforms.
func platformsFromStrings(names []string) []v1.Platform {
	if len(names) == 0 {
		return nil
	}
	res := make([]v1.Platform, 0, len(names))
	for _, s := range names {
		p := parsePlatformString(s)
		res = append(res, p)
	}
	return res
}

func parsePlatformString(s string) v1.Platform {
	// Expect os/arch[/variant]
	parts := strings.Split(s, "/")
	p := v1.Platform{}
	if len(parts) > 0 {
		p.OS = parts[0]
	}
	if len(parts) > 1 {
		p.Architecture = parts[1]
	}
	if len(parts) > 2 {
		p.Variant = parts[2]
	}
	return p
}

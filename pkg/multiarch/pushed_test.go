package multiarch

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestPlatformsFromIndexManifest(t *testing.T) {
	idxm := &v1.IndexManifest{
		Manifests: []v1.Descriptor{
			// valid image with platform
			{
				MediaType: types.OCIManifestSchema1,
				Platform:  &v1.Platform{OS: "linux", Architecture: "amd64"},
			},
			// missing platform -> skipped
			{
				MediaType: types.OCIManifestSchema1,
				Platform:  nil,
			},
			// non-image mediatype -> skipped
			{
				MediaType: types.OCIImageIndex,
				Platform:  &v1.Platform{OS: "linux", Architecture: "arm64"},
			},
			// attestation -> skipped
			{
				MediaType:   types.OCIManifestSchema1,
				Platform:    &v1.Platform{OS: "unknown", Architecture: "unknown"},
				Annotations: map[string]string{ReferenceTypeAnnotation: ReferenceTypeAttestation},
			},
			// another valid image
			{
				MediaType: types.OCIManifestSchema1,
				Platform:  &v1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	}

	plats := platformsFromIndexManifest(idxm)
	if len(plats) != 2 {
		// Expect only the two valid entries
		t.Fatalf("expected 2 platforms, got %d: %#v", len(plats), plats)
	}
	if plats[0].OS != "linux" || plats[0].Architecture != "amd64" {
		t.Errorf("unexpected first platform: %#v", plats[0])
	}
	if plats[1].OS != "linux" || plats[1].Architecture != "arm64" {
		t.Errorf("unexpected second platform: %#v", plats[1])
	}
}

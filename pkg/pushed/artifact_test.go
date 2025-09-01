package pushed

import (
	"encoding/json"
	"fmt"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/types"
	. "github.com/onsi/gomega"
)

func TestNewSingleImage_AndJSONRoundTrip(t *testing.T) {
	RegisterTestingT(t)
	// Build a simple random image and derive its digest
	img, err := random.Image(512, 1)
	Expect(err).NotTo(HaveOccurred())
	digest, err := img.Digest()
	Expect(err).NotTo(HaveOccurred())

	platform := &v1.Platform{OS: "linux", Architecture: "amd64"}
	tag := "localhost:22500/localdir-arm64:v0.5.5-37-g0a9a4c9"

	a, err := NewSingleImage(tag, digest, img, platform)
	Expect(err).NotTo(HaveOccurred())

	// Assert basics
	Expect(a.ImageName).To(Equal("localhost:22500/localdir-arm64"))
	Expect(a.TagRef).To(Equal(fmt.Sprintf("%s@%s", tag, digest.String())))

	// Internal fields
	Expect(a.reference.Identifier()).To(Equal("v0.5.5-37-g0a9a4c9"))
	Expect(a.hash.String()).To(Equal(digest.String()))

	// Config digest captured when available
	if cfg, err := img.ConfigName(); err == nil {
		Expect(a.singleImageConfigHash.String()).To(Equal(cfg.String()))
	}

	// JSON round-trip: also assert mediaType and platforms using generic parsing
	raw, err := json.Marshal(a)
	Expect(err).NotTo(HaveOccurred())
	// Generic parse to record how fields look without typed structs
	var generic map[string]any
	Expect(json.Unmarshal(raw, &generic)).To(Succeed())
	// mediaType as string
	mt, ok := generic["mediaType"].(string)
	Expect(ok).To(BeTrue())
	Expect(mt).NotTo(BeEmpty())
	// platforms as array of strings
	pf, ok := generic["platforms"].([]any)
	Expect(ok).To(BeTrue())
	Expect(pf).To(HaveLen(1))
	Expect(pf).To(ContainElement(Equal("linux/amd64")))
	var b Artifact
	Expect(json.Unmarshal(raw, &b)).To(Succeed())

	Expect(b.ImageName).To(Equal(a.ImageName))
	Expect(b.TagRef).To(Equal(a.TagRef))
	Expect(b.MediaType).To(Equal(a.MediaType))
	Expect(b.Platforms).To(HaveLen(1))
	Expect(b.Platforms[0].String()).To(Equal("linux/amd64"))
	// Private fields reconstructed
	Expect(b.reference.Identifier()).To(Equal("v0.5.5-37-g0a9a4c9"))
	Expect(b.hash.String()).To(Equal(digest.String()))
	// Config hash cannot be reconstructed from JSON; expect zero value
	Expect(b.singleImageConfigHash).To(Equal(v1.Hash{}))
}

func TestNewIndexImage_AndJSONRoundTrip(t *testing.T) {
	RegisterTestingT(t)
	// Two random child images for different platforms
	amdImg, err := random.Image(256, 1)
	Expect(err).NotTo(HaveOccurred())
	armImg, err := random.Image(256, 1)
	Expect(err).NotTo(HaveOccurred())

	// Build an OCI index with explicit platform descriptors
	var idx v1.ImageIndex = empty.Index
	idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
		Add: amdImg,
		Descriptor: v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Platform:  &v1.Platform{OS: "linux", Architecture: "amd64"},
		},
	})
	idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
		Add: armImg,
		Descriptor: v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Platform:  &v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"},
		},
	})
	// Ensure index media type is OCI image index
	idx = mutate.IndexMediaType(idx, types.OCIImageIndex)

	digest, err := idx.Digest()
	Expect(err).NotTo(HaveOccurred())

	tag := "localhost:22500/localdir-app:v0.5.5-37-g0a9a4c9"
	a, err := NewIndexImage(tag, digest, idx)
	Expect(err).NotTo(HaveOccurred())

	// Assert basics
	Expect(a.ImageName).To(Equal("localhost:22500/localdir-app"))
	Expect(a.TagRef).To(Equal(fmt.Sprintf("%s@%s", tag, digest.String())))
	// Platforms are asserted via generic JSON below

	// Internal fields
	Expect(a.reference.Identifier()).To(Equal("v0.5.5-37-g0a9a4c9"))
	Expect(a.hash.String()).To(Equal(digest.String()))

	// JSON round-trip and generic assertion of mediaType/platforms
	raw, err := json.Marshal(a)
	Expect(err).NotTo(HaveOccurred())
	var generic map[string]any
	Expect(json.Unmarshal(raw, &generic)).To(Succeed())
	mt, ok := generic["mediaType"].(string)
	Expect(ok).To(BeTrue())
	Expect(mt).NotTo(BeEmpty())
	pf, ok := generic["platforms"].([]any)
	Expect(ok).To(BeTrue())
	Expect(pf).To(HaveLen(2))
	Expect(pf).To(ContainElement("linux/amd64"))
	Expect(pf).To(ContainElement("linux/arm64/v8"))
	var b Artifact
	Expect(json.Unmarshal(raw, &b)).To(Succeed())

	Expect(b.ImageName).To(Equal(a.ImageName))
	Expect(b.TagRef).To(Equal(a.TagRef))
	Expect(b.MediaType).To(Equal(a.MediaType))
	Expect(b.Platforms).To(HaveLen(2))
	// Private fields reconstructed
	Expect(b.reference.Identifier()).To(Equal("v0.5.5-37-g0a9a4c9"))
	Expect(b.hash.String()).To(Equal(digest.String()))
	// singleImageConfigHash must remain empty for index
	Expect(b.singleImageConfigHash).To(Equal(v1.Hash{}))
}

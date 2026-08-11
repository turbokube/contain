package multiarch_test

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/multiarch"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

func desc(p *v1.Platform) v1.Descriptor {
	return v1.Descriptor{Platform: p}
}

func platformPtr(os, arch, variant string) *v1.Platform {
	return &v1.Platform{OS: os, Architecture: arch, Variant: variant}
}

func TestMatchPlatformsForAppend(t *testing.T) {
	RegisterTestingT(t)
	c := schema.ContainConfig{
		Platforms: []string{
			"linux/amd64",
			"linux/arm64/v8",
		},
	}
	m, err := multiarch.MatchPlatformsForAppend(c)
	Expect(err).NotTo(HaveOccurred())
	Expect(m(desc(platformPtr("linux", "amd64", "")))).To(BeTrue())
	Expect(m(desc(platformPtr("linux", "arm64", "v8")))).To(BeTrue())
	// the base image declaring arm64 without a variant is the same platform
	// as the configured arm64/v8, so it now matches
	Expect(m(desc(platformPtr("linux", "arm64", "")))).To(BeTrue())
	// normalization does not widen: these stay distinct
	Expect(m(desc(platformPtr("linux", "amd64", "v8")))).To(BeFalse())
	Expect(m(desc(platformPtr("linux", "arm64", "v7")))).To(BeFalse())
	Expect(m(desc(platformPtr("linux", "arm64", "v9")))).To(BeFalse())
	Expect(m(desc(platformPtr("linux", "arm", "v8")))).To(BeFalse())
	Expect(m(desc(platformPtr("darwin", "amd64", "")))).To(BeFalse())

	// the reported case: config without the variant, base with it
	c2 := schema.ContainConfig{Platforms: []string{"linux/arm64"}}
	m2, err := multiarch.MatchPlatformsForAppend(c2)
	Expect(err).NotTo(HaveOccurred())
	Expect(m2(desc(platformPtr("linux", "arm64", "")))).To(BeTrue())
	Expect(m2(desc(platformPtr("linux", "arm64", "v8")))).To(BeTrue())
	Expect(m2(desc(platformPtr("linux", "arm64", "v8.1")))).To(BeFalse())

	// linux/arm means arm/v7 exactly, not "any arm". A base declaring v5, v6
	// and v7 must contribute one child, not three.
	c3 := schema.ContainConfig{Platforms: []string{"linux/arm"}}
	m3, err := multiarch.MatchPlatformsForAppend(c3)
	Expect(err).NotTo(HaveOccurred())
	Expect(m3(desc(platformPtr("linux", "arm", "v7")))).To(BeTrue())
	Expect(m3(desc(platformPtr("linux", "arm", "")))).To(BeTrue())
	Expect(m3(desc(platformPtr("linux", "arm", "v5")))).To(BeFalse())
	Expect(m3(desc(platformPtr("linux", "arm", "v6")))).To(BeFalse())

	// no platforms config means take whatever the base has
	m4, err := multiarch.MatchPlatformsForAppend(schema.ContainConfig{})
	Expect(err).NotTo(HaveOccurred())
	Expect(m4(desc(platformPtr("linux", "s390x", "")))).To(BeTrue())
	Expect(m4(desc(nil))).To(BeTrue())

	// a descriptor without a platform cannot satisfy a platforms config
	Expect(m(desc(nil))).To(BeFalse())

	_, err = multiarch.MatchPlatformsForAppend(schema.ContainConfig{Platforms: []string{"a/b/c/d"}})
	Expect(err).To(HaveOccurred())
}

func TestUnmatchedPlatforms(t *testing.T) {
	RegisterTestingT(t)
	base := []v1.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}

	// no config platforms means "whatever the base has"
	u, err := multiarch.UnmatchedPlatforms(nil, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(BeEmpty())

	u, err = multiarch.UnmatchedPlatforms([]string{"linux/amd64", "linux/arm64"}, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(BeEmpty())

	// a subset of the base is a valid request
	u, err = multiarch.UnmatchedPlatforms([]string{"linux/arm64"}, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(BeEmpty())

	u, err = multiarch.UnmatchedPlatforms([]string{"linux/amd64", "linux/s390x"}, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(Equal([]string{"linux/s390x"}))

	// the variant spellings agree in both directions, so neither is unmatched
	u, err = multiarch.UnmatchedPlatforms([]string{"linux/arm64/v8"}, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(BeEmpty())

	u, err = multiarch.UnmatchedPlatforms([]string{"linux/arm64"}, []v1.Platform{
		{OS: "linux", Architecture: "arm64", Variant: "v8"},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(BeEmpty())

	// still reported when the variant is a real feature level
	u, err = multiarch.UnmatchedPlatforms([]string{"linux/arm64/v8.1"}, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(Equal([]string{"linux/arm64/v8.1"}))

	// the reported spelling is the config's, not the normalized form, so the
	// error names what the user wrote
	u, err = multiarch.UnmatchedPlatforms([]string{"linux/aarch64/v9"}, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(Equal([]string{"linux/aarch64/v9"}))

	_, err = multiarch.UnmatchedPlatforms([]string{"a/b/c/d"}, base)
	Expect(err).To(HaveOccurred())
}

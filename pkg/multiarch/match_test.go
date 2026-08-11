package multiarch_test

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/multiarch"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

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
	Expect(m(v1.Descriptor{
		Platform: &v1.Platform{
			OS:           "linux",
			Architecture: "amd64",
		},
	})).To(BeTrue())
	Expect(m(v1.Descriptor{
		Platform: &v1.Platform{
			OS:           "linux",
			Architecture: "amd64",
			Variant:      "v8",
		},
	})).To(BeFalse())
	Expect(m(v1.Descriptor{
		Platform: &v1.Platform{
			OS:           "linux",
			Architecture: "arm64",
			Variant:      "v8",
		},
	})).To(BeTrue())
	Expect(m(v1.Descriptor{
		Platform: &v1.Platform{
			OS:           "linux",
			Architecture: "arm64",
			Variant:      "v7",
		},
	})).To(BeFalse())
	// this would be the base image having no variant set
	Expect(m(v1.Descriptor{
		Platform: &v1.Platform{
			OS:           "linux",
			Architecture: "arm64",
		},
		// we don't know if we can specialize the base image's platform
	})).To(BeFalse())
	c2 := schema.ContainConfig{
		Platforms: []string{
			"linux/arm64",
		},
	}
	m2, err := multiarch.MatchPlatformsForAppend(c2)
	Expect(err).NotTo(HaveOccurred())
	Expect(m2(v1.Descriptor{
		Platform: &v1.Platform{
			OS:           "linux",
			Architecture: "arm64",
		},
	})).To(BeTrue())
	// this would be the config having no variant but the base image having it
	Expect(m2(v1.Descriptor{
		Platform: &v1.Platform{
			OS:           "linux",
			Architecture: "arm64",
			Variant:      "v8",
		},
		// we don't know if we can generalize the base image's platform
	})).To(BeFalse())
}

func TestPlatformString(t *testing.T) {
	RegisterTestingT(t)
	// index descriptors may omit platform, and v1.Platform.String has a value
	// receiver, so the nil case must not reach it
	Expect(multiarch.PlatformString(nil)).To(Equal("<none>"))
	// a non-nil but empty platform is the caller's business, v1 renders it as ""
	Expect(multiarch.PlatformString(&v1.Platform{})).To(Equal(""))
	Expect(multiarch.PlatformString(&v1.Platform{
		OS:           "linux",
		Architecture: "arm64",
	})).To(Equal("linux/arm64"))
	Expect(multiarch.PlatformString(&v1.Platform{
		OS:           "linux",
		Architecture: "arm64",
		Variant:      "v8",
	})).To(Equal("linux/arm64/v8"))
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

	// variant spelling is compared exactly, as MatchPlatformsForAppend does
	u, err = multiarch.UnmatchedPlatforms([]string{"linux/arm64/v8"}, base)
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(Equal([]string{"linux/arm64/v8"}))

	// and in the other direction
	u, err = multiarch.UnmatchedPlatforms([]string{"linux/arm64"}, []v1.Platform{
		{OS: "linux", Architecture: "arm64", Variant: "v8"},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(u).To(Equal([]string{"linux/arm64"}))

	_, err = multiarch.UnmatchedPlatforms([]string{"a/b/c/d"}, base)
	Expect(err).To(HaveOccurred())
}

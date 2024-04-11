package multiarch_test

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/multiarch"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

func TestNewPlatformsMatcher(t *testing.T) {
	RegisterTestingT(t)
	c := schema.ContainConfig{
		Platforms: []string{
			"linux/amd64",
			"linux/arm64/v8",
		},
	}
	m, err := multiarch.NewPlatformsMatcher(c)
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
	})).To(BeFalse())
	c2 := schema.ContainConfig{
		Platforms: []string{
			"linux/arm64",
		},
	}
	m2, err := multiarch.NewPlatformsMatcher(c2)
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
	})).To(BeFalse())
}

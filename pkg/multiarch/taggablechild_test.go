package multiarch_test

import (
	"fmt"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/multiarch"
)

func TestTaggableChild(t *testing.T) {
	RegisterTestingT(t)

	// here we actually want go-containerregistry to pick an achitecture from an index
	example1, err := remote.Image(name.MustParseReference("registry:2.8.3@sha256:f4e1b878d4bc40a1f65532d68c94dcfbab56aa8cba1f00e355a206e7f6cc9111"))
	Expect(err).NotTo(HaveOccurred())
	m1, err := example1.Manifest()
	Expect(err).NotTo(HaveOccurred())
	t1, err := multiarch.NewTaggableChild(*m1)
	Expect(err).NotTo(HaveOccurred())
	m1digest, err := example1.Digest()
	Expect(err).NotTo(HaveOccurred())
	m1size, err := example1.Size()
	Expect(err).NotTo(HaveOccurred())
	m1raw, err := example1.RawManifest()
	Expect(err).NotTo(HaveOccurred())
	t1mediaType, err := t1.MediaType()
	Expect(err).NotTo(HaveOccurred())
	t1digest, err := t1.Digest()
	Expect(err).NotTo(HaveOccurred())
	t1size, err := t1.Size()
	Expect(err).NotTo(HaveOccurred())
	t1raw, err := t1.RawManifest()
	Expect(err).NotTo(HaveOccurred())
	fmt.Println(string(t1raw))
	fmt.Println(string(m1raw))
	Expect(string(t1raw)).To(Equal(string(m1raw)))
	Expect(t1mediaType).To(Equal(m1.MediaType))
	Expect(t1digest).To(Equal(m1digest))
	Expect(t1size).To(Equal(m1size))
}

package multiarch_test

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/types"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/multiarch"
)

func TestTaggableIndex(t *testing.T) {
	RegisterTestingT(t)
	i := empty.Index
	ti, err := multiarch.NewTaggableIndex(i)
	Expect(err).NotTo(HaveOccurred())
	m, err := ti.MediaType()
	Expect(err).NotTo(HaveOccurred())
	Expect(m).To(Equal(types.OCIImageIndex))
	r, err := ti.RawManifest()
	Expect(err).NotTo(HaveOccurred())
	Expect(string(r)).To(Equal("{\"schemaVersion\":2,\"mediaType\":\"application/vnd.oci.image.index.v1+json\",\"manifests\":[]}"))
	d, err := ti.Digest()
	Expect(err).NotTo(HaveOccurred())
	Expect(d.String()).To(Equal("sha256:dff9de10919148711140d349bf03f1a99eb06f94b03e51715ccebfa7cdc518e2"))
	s, err := ti.Size()
	Expect(err).NotTo(HaveOccurred())
	Expect(s).To(BeEquivalentTo(88))
}

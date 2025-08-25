package multiarch_test

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/localdir"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

func MockLayer(filepath string, content string) (v1.Layer, appender.AppendResultLayer) {
	filemap := make(map[string][]byte)
	filemap[filepath] = []byte(content)
	layer, err := localdir.Layer(filemap, schema.LayerAttributes{})
	Expect(err).NotTo(HaveOccurred())
	m, err := layer.MediaType()
	Expect(err).NotTo(HaveOccurred())
	h, err := layer.Digest()
	Expect(err).NotTo(HaveOccurred())
	s, err := layer.Size()
	Expect(err).NotTo(HaveOccurred())
	return layer, appender.AppendResultLayer{
		MediaType: m,
		Digest:    h,
		Size:      s,
	}
}

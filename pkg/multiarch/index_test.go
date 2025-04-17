package multiarch_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/localdir"
	"github.com/turbokube/contain/pkg/multiarch"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestIndexManifests(t *testing.T) {
	t.Skip("this test was based on the idea of a prototype append whose layer meta could be used to derive the rest")
	RegisterTestingT(t)

	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := testcases.NewTestregistry(ctx)
	r.Start()

	index, err := multiarch.NewFromMultiArchBase(schema.ContainConfig{
		Base: fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest@sha256:c6dde17b43016c18361cf6b2db724b84312f074f9cb332438bc3908ac603f995", r.Host),
	}, &r.Config)
	if err != nil {
		t.Fatal(err)
	}

	prototype, err := index.GetPrototypeBase()
	if err != nil {
		t.Error(err)
	}
	// we don't care which arch that is prototype, but the first item in the manifests array happens to be amd64
	Expect(prototype.DigestStr()).To(Equal("sha256:88b8e36da2fe3947b813bd52473319c3fb2e7637692ff4c499fa8bd878241852"))
	Expect(prototype.String()).To(Equal(fmt.Sprintf("%s/contain-test/baseimage-multiarch1@sha256:88b8e36da2fe3947b813bd52473319c3fb2e7637692ff4c499fa8bd878241852", r.Host)))

	testtag, err := name.ParseReference(
		fmt.Sprintf("%s/contain-test/imageindex-test1", r.Host),
		r.Config.CraneOptions.Name...,
	)
	Expect(err).NotTo(HaveOccurred())

	// registry will check that referenced manifests exist, so we need to push an actual image
	prototypePushed := mutate.ConfigMediaType(empty.Image, types.OCIManifestSchema1)
	layer1, added1 := MockLayer("test1.txt", "test")
	prototypePushed, err = mutate.AppendLayers(prototypePushed, layer1)
	Expect(err).NotTo(HaveOccurred())
	remote.Put(testtag, prototypePushed, r.Config.CraneOptions.Remote...)
	prototypePushedDigest, err := prototypePushed.Digest()
	Expect(err).NotTo(HaveOccurred())

	// mock result from appender package
	result := appender.AppendResult{
		Hash: prototypePushedDigest,
		Pushed: mutate.IndexAddendum{
			Add:        prototypePushed,
			Descriptor: v1.Descriptor{
				// should we set platform here?
			},
		},
		AddedManifestLayers: []appender.AppendResultLayer{added1},
	}

	hash, err := index.PushIndex(testtag, result, &r.Config)
	Expect(err).NotTo(HaveOccurred())

	Expect(hash.String()).To(Equal("sha256:7fe691a765daed6cfae1536668796535df1190d8fa00486f0580590836bdef05"))

	// avoid remote.Image because it will probably unwrap index and use default platform, or something
	pushed, err := remote.Get(testtag, r.Config.CraneOptions.Remote...)
	Expect(err).NotTo(HaveOccurred())
	pi, err := pushed.ImageIndex()
	Expect(err).NotTo(HaveOccurred())
	pm, err := pi.IndexManifest()
	Expect(err).NotTo(HaveOccurred())
	Expect(pm.MediaType).To(Equal(types.OCIImageIndex))

	// TODO verify that multiarch actually added the other image(s)
	Expect(len(pm.Manifests)).To(Equal(2))
}

func MockLayer(filepath string, content string) (v1.Layer, appender.AppendResultLayer) {
	filemap := make(map[string][]byte)
	filemap[filepath] = []byte(content)
	// Create empty maps for the new Layer function signature
	dirmap := make(map[string]bool)
	symlinkMap := make(map[string]bool)
	modeMap := make(map[string]int64)
	layer, err := localdir.Layer(filemap, dirmap, symlinkMap, modeMap, schema.LayerAttributes{})
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

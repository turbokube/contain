package multiarch_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/multiarch"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestImageIndex(t *testing.T) {
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

	// mock result from appender package
	result := appender.AppendResult{
		Hash: testcases.NewMockHash("sha256:50c393f158c3de2db92fa9661bfb00eda5b67c3a777c88524ed3417509631625"),
		Pushed: mutate.IndexAddendum{
			Add: testcases.NewMockDescribable("", types.OCIManifestSchema1, 1234),
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{
					Architecture: "386",
					OS:           "linux",
				},
			},
		},
		AddedManifestLayers: []appender.AppendResultLayer{
			{
				MediaType: types.OCILayer,
				Size:      1,
				Digest:    testcases.NewMockHash("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865"),
			},
		},
	}
	Expect(err).NotTo(HaveOccurred())

	testtag, err := name.ParseReference(
		fmt.Sprintf("%s/contain-test/imageindex-test1", r.Host),
		r.Config.CraneOptions.Name...,
	)
	Expect(err).NotTo(HaveOccurred())
	hash, err := index.PushIndex(testtag, result, &r.Config)
	Expect(err).NotTo(HaveOccurred())

	Expect(hash.String()).NotTo(Equal("foo"))

	pushed, err := remote.Head(testtag, r.Config.CraneOptions.Remote...)
	Expect(err).NotTo(HaveOccurred())
	Expect(pushed.MediaType).To(Equal(types.OCIImageIndex))

	// TODO make sure all manifests are pushed
}

package appender_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/localdir"
	v1 "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
)

func TestAppender(t *testing.T) {
	RegisterTestingT(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := testcases.NewTestregistry(ctx)

	if err := r.Start(); err != nil {
		t.Fatalf("testregistry start %v", err)
	}

	base, err := name.ParseReference(
		// the amd64 manifest in contain-test/baseimage-multiarch1:noattest
		fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest@sha256:88b8e36da2fe3947b813bd52473319c3fb2e7637692ff4c499fa8bd878241852", r.Host),
		r.Config.CraneOptions.Name...,
	)
	if err != nil {
		t.Fatal(err)
	}

	tag, err := name.ParseReference(
		fmt.Sprintf("%s/contain-test/append-test:1", r.Host),
		r.Config.CraneOptions.Name...,
	)
	if err != nil {
		t.Fatal(err)
	}

	a, err := appender.New(base.(name.Digest), &r.Config, tag.(name.Tag))
	if err != nil {
		t.Fatal(err)
	}

	filemap := map[string][]byte{
		"test.txt": []byte("test"),
	}
	layer, err := localdir.Layer(filemap, v1.LayerAttributes{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Append(layer)
	if err != nil {
		t.Error(err)
	}

	tagged, err := remote.Get(tag, r.Config.CraneOptions.Remote...)
	if err != nil {
		t.Errorf("%s wasn't pushed? %v", tag, err)
	}
	Expect(tagged.MediaType).To(Equal(types.OCIManifestSchema1))
	Expect(tagged.Digest.String()).To(Equal("sha256:002e71a20d689b4cca13e3a2a23f740e5b54faf00d6e0ab85ec5bcf7f71f632b"))
	// what's this digest for?
	Expect(tagged.RawManifest()).To(ContainSubstring("sha256:d73a3b86e6907bc6a65ad8ad0c3ea208c831f1f82e9c71f3ec800bafdc052137"))

	Expect(result.Hash.String()).To(Equal("sha256:002e71a20d689b4cca13e3a2a23f740e5b54faf00d6e0ab85ec5bcf7f71f632b"))
	Expect(result.AddedManifestLayers).To(HaveLen(1))
	added := result.AddedManifestLayers[0]
	Expect(added.MediaType).To(Equal(types.DockerLayer))
	Expect(added.Digest.String()).To(Equal("sha256:72b763668602c1aaab0c817a9478a823ce68e3de59239dad3561c17452dda66b"))
}

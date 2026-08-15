package ocipush_test

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/turbokube/contain/pkg/ocipush"
)

// TestMirror copies a multi-manifest index from a plain source registry to an
// ext-extension destination, pinned by digest, and verifies preservation.
func TestMirror(t *testing.T) {
	srcServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer srcServer.Close()
	srcHost := strings.TrimPrefix(srcServer.URL, "http://")

	fake := &extFake{reg: registry.New(registry.Logger(log.New(io.Discard, "", 0))), staged: map[string]map[int][]byte{}}
	dstServer := httptest.NewServer(fake)
	defer dstServer.Close()
	fake.serverURL = dstServer.URL
	dstHost := strings.TrimPrefix(dstServer.URL, "http://")

	ii, err := random.Index(3500, 2, 2) // layers > fakePartSize for multipart
	if err != nil {
		t.Fatal(err)
	}
	want, err := ii.Digest()
	if err != nil {
		t.Fatal(err)
	}
	srcRef, err := name.ParseReference(srcHost + "/yolean/app:build1")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.WriteIndex(srcRef, ii, remote.WithAuth(authn.Anonymous)); err != nil {
		t.Fatal(err)
	}

	// tag@digest source form, like kustomize newTag pinning
	err = ocipush.Mirror(context.Background(),
		srcHost+"/yolean/app:build1@"+want.String(),
		dstHost+"/yolean/app:build1",
		ocipush.MirrorOptions{
			Src: ocipush.Options{Auth: authn.Anonymous},
			Dst: ocipush.Options{Auth: authn.Anonymous, ExtThreshold: 1},
		})
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	if fake.uploads == 0 {
		t.Errorf("destination extension not exercised")
	}

	dstRef, err := name.ParseReference(dstHost + "/yolean/app:build1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := remote.Get(dstRef, remote.WithAuth(authn.Anonymous))
	if err != nil {
		t.Fatalf("pull mirrored: %v", err)
	}
	if got.Digest.String() != want.String() {
		t.Errorf("digest not preserved: src %s dst %s", want, got.Digest)
	}

	// mirroring again must be a cheap no-op path (blobs exist), still correct
	err = ocipush.Mirror(context.Background(),
		srcHost+"/yolean/app:build1@"+want.String(),
		dstHost+"/yolean/app:build1",
		ocipush.MirrorOptions{
			Src: ocipush.Options{Auth: authn.Anonymous},
			Dst: ocipush.Options{Auth: authn.Anonymous, ExtThreshold: 1},
		})
	if err != nil {
		t.Fatalf("re-mirror: %v", err)
	}

	// a wrong pinned digest must refuse to mirror
	wrong := strings.Replace(want.String(), want.String()[7:8], "0", 1)
	if want.String()[7:8] == "0" {
		wrong = strings.Replace(want.String(), "0", "1", 1)
	}
	err = ocipush.Mirror(context.Background(),
		srcHost+"/yolean/app:build1@"+wrong,
		dstHost+"/yolean/app:bad",
		ocipush.MirrorOptions{
			Src: ocipush.Options{Auth: authn.Anonymous},
			Dst: ocipush.Options{Auth: authn.Anonymous},
		})
	if err == nil {
		t.Errorf("mirror with wrong pinned digest should fail")
	}
}

package contain_test

import (
	"encoding/json"
	"os"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/turbokube/contain/pkg/contain"
	"github.com/turbokube/contain/pkg/multiarch"
)

func TestBuildOutput(t *testing.T) {
	h1, err := v1.NewHash("sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f")
	if err != nil {
		t.Error(err)
	}
	p1 := multiarch.Pushed{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    h1,
		Platforms: []v1.Platform{
			{OS: "linux", Architecture: "amd64"},
			{OS: "linux", Architecture: "arm64", Variant: "v8"},
		},
	}

	t.Run("image with registry", func(t *testing.T) {
		o, err := contain.NewBuildOutput("localhost:1234/test/foo:latest", p1)
		if err != nil {
			t.Error(err)
		}
		if len(o.Skaffold.Builds) != 1 {
			t.Errorf("%d builds", len(o.Skaffold.Builds))
		}
		if o.Skaffold.Builds[0].Tag != "localhost:1234/test/foo:latest@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f" {
			t.Errorf("ref %s", o.Skaffold.Builds[0].Tag)
		}
		if o.Skaffold.Builds[0].ImageName != "localhost:1234/test/foo" {
			t.Errorf("name %s", o.Skaffold.Builds[0].ImageName)
		}
		f, err := os.CreateTemp("", ".json")
		if err != nil {
			t.Error(err)
		}
		defer os.Remove(f.Name())
		o.WriteSkaffoldJSON(f)
		jsonBytes, err := os.ReadFile(f.Name())
		if err != nil {
			t.Error(err)
		}
		if string(jsonBytes) != "{\"builds\":[{\"imageName\":\"localhost:1234/test/foo\",\"tag\":\"localhost:1234/test/foo:latest@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f\",\"mediaType\":\"application/vnd.oci.image.manifest.v1+json\",\"platforms\":[\"linux/amd64\",\"linux/arm64/v8\"]}]}" {
			t.Errorf("json %s", jsonBytes)
		}
		http := o.Skaffold.Builds[0].Http()
		if http.Host != "localhost:1234" {
			t.Errorf("host %s", http.Host)
		}
		if http.Repository != "test/foo" {
			t.Errorf("repository %s", http.Repository)
		}
		if http.Tag != "latest" {
			t.Errorf("tag %s", http.Tag)
		}
		if http.Hash != h1 {
			t.Errorf("hash %v", http.Hash)
		}
	})

	t.Run("image with default registry", func(t *testing.T) {
		o, err := contain.NewBuildOutput("test/foo:a", p1)
		if err != nil {
			t.Error(err)
		}
		if len(o.Skaffold.Builds) != 1 {
			t.Errorf("%d builds", len(o.Skaffold.Builds))
		}
		if o.Skaffold.Builds[0].Tag != "test/foo:a@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f" {
			t.Errorf("ref %s", o.Skaffold.Builds[0].Tag)
		}
		if o.Skaffold.Builds[0].ImageName != "test/foo" {
			t.Errorf("name %s", o.Skaffold.Builds[0].ImageName)
		}
		http := o.Skaffold.Builds[0].Http()
		if http.Host != "index.docker.io" {
			t.Errorf("default host %s", http.Host)
		}
		if http.Repository != "test/foo" {
			t.Errorf("repository %s", http.Repository)
		}
		if http.Tag != "a" {
			t.Errorf("tag %s", http.Tag)
		}
		if http.Hash != h1 {
			t.Errorf("hash %v", http.Hash)
		}
	})

	t.Run("image with default tag", func(t *testing.T) {
		o, err := contain.NewBuildOutput("test/foo", p1)
		if err != nil {
			t.Error(err)
		}
		if len(o.Skaffold.Builds) != 1 {
			t.Errorf("%d builds", len(o.Skaffold.Builds))
		}
		if o.Skaffold.Builds[0].Tag != "test/foo@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f" {
			t.Errorf("ref %s", o.Skaffold.Builds[0].Tag)
		}
		if o.Skaffold.Builds[0].ImageName != "test/foo" {
			t.Errorf("name %s", o.Skaffold.Builds[0].ImageName)
		}
		http := o.Skaffold.Builds[0].Http()
		if http.Host != "index.docker.io" {
			t.Errorf("default host %s", http.Host)
		}
		if http.Repository != "test/foo" {
			t.Errorf("repository %s", http.Repository)
		}
		if http.Tag != "latest" {
			t.Errorf("default tag %s", http.Tag)
		}
		if http.Hash != h1 {
			t.Errorf("hash %v", http.Hash)
		}
	})

	t.Run("buildctl metadata output", func(t *testing.T) {
		o, err := contain.NewBuildOutput("example.net/yolean/g5y-sidecar:daa3b6df7f58f7644a4ecb129af3d6f70653127c-dirty-205649", p1)
		if err != nil {
			t.Error(err)
		}
		if o.Buildctl == nil {
			t.Error("Buildctl metadata should not be nil")
		}
		if o.Buildctl.ImageName != "example.net/yolean/g5y-sidecar:daa3b6df7f58f7644a4ecb129af3d6f70653127c-dirty-205649" {
			t.Errorf("image name %s", o.Buildctl.ImageName)
		}
		if o.Buildctl.ContainerImageDigest != h1.String() {
			t.Errorf("digest %s", o.Buildctl.ContainerImageDigest)
		}

		f, err := os.CreateTemp("", ".json")
		if err != nil {
			t.Error(err)
		}
		defer os.Remove(f.Name())
		o.WriteBuildctlJSON(f)
		jsonBytes, err := os.ReadFile(f.Name())
		if err != nil {
			t.Error(err)
		}
		// Basic check that it's valid JSON with expected fields
		var metadata map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &metadata); err != nil {
			t.Errorf("invalid JSON: %v", err)
		}
		if metadata["containerimage.digest"] != h1.String() {
			t.Errorf("missing or wrong containerimage.digest")
		}
		if metadata["image.name"] != "example.net/yolean/g5y-sidecar:daa3b6df7f58f7644a4ecb129af3d6f70653127c-dirty-205649" {
			t.Errorf("missing or wrong image.name")
		}
	})

	t.Run("bad input", func(t *testing.T) {
		var err error
		if _, err = contain.NewBuildOutput("", p1); err == nil {
			t.Error("Should have err'd on empty")
		}
		if _, err = contain.NewBuildOutput(":tag", p1); err == nil {
			t.Error("Should have err'd on tag only")
		}
		if _, err = contain.NewBuildOutput("test/foo@123", p1); err == nil {
			t.Error("Should have err'd on invalid digest")
		}
	})

}

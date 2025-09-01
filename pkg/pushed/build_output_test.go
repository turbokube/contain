package pushed

import (
	"encoding/json"
	"os"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestBuildOutput(t *testing.T) {
	h1, err := v1.NewHash("sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f")
	if err != nil {
		t.Error(err)
	}
	a := &Artifact{
		ImageName: "localhost:1234/test/foo",
		TagRef:    "localhost:1234/test/foo:latest@" + h1.String(),
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Platforms: []v1.Platform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64", Variant: "v8"}},
		hash:      h1,
	}
	// Rebuild internal reference for Http() usage
	if err := json.Unmarshal([]byte(`{"imageName":"`+a.ImageName+`","tag":"`+a.TagRef+`","mediaType":"`+string(a.MediaType)+`","platforms":["linux/amd64","linux/arm64/v8"]}`), a); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	t.Run("image with registry", func(t *testing.T) {
		o, err := NewBuildOutput("localhost:1234/test/foo:latest", a)
		if err != nil {
			t.Error(err)
		}
		if len(o.Skaffold.Builds) != 1 {
			t.Errorf("%d builds", len(o.Skaffold.Builds))
		}
		if o.Skaffold.Builds[0].TagRef != "localhost:1234/test/foo:latest@"+h1.String() {
			t.Errorf("ref %s", o.Skaffold.Builds[0].TagRef)
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
		expected := "{" +
			"\"builds\":[{" +
			"\"imageName\":\"localhost:1234/test/foo\"," +
			"\"tag\":\"localhost:1234/test/foo:latest@" + h1.String() + "\"," +
			"\"mediaType\":\"application/vnd.oci.image.manifest.v1+json\"," +
			"\"platforms\":[\"linux/amd64\",\"linux/arm64/v8\"]}]}"
		if string(jsonBytes) != expected {
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
		// Create a fresh artifact with no explicit registry
		var a2 Artifact
		if err := json.Unmarshal([]byte(`{"imageName":"test/foo","tag":"test/foo:a@`+h1.String()+`","mediaType":"application/vnd.oci.image.manifest.v1+json","platforms":["linux/amd64","linux/arm64/v8"]}`), &a2); err != nil {
			t.Fatalf("unmarshal a2: %v", err)
		}
		o, err := NewBuildOutput("test/foo:a", &a2)
		if err != nil {
			t.Error(err)
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
		var a3 Artifact
		if err := json.Unmarshal([]byte(`{"imageName":"test/foo","tag":"test/foo@`+h1.String()+`","mediaType":"application/vnd.oci.image.manifest.v1+json","platforms":["linux/amd64","linux/arm64/v8"]}`), &a3); err != nil {
			t.Fatalf("unmarshal a3: %v", err)
		}
		o, err := NewBuildOutput("test/foo", &a3)
		if err != nil {
			t.Error(err)
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
		o, err := NewBuildOutput("example.net/yolean/g5y-sidecar:daa3b6df7f58f7644a4ecb129af3d6f70653127c-dirty-205649", a)
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
}

package contain_test

import (
	"os"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/turbokube/contain/pkg/contain"
)

func TestBuildOutput(t *testing.T) {
	h1, err := v1.NewHash("sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f")
	if err != nil {
		t.Error(err)
	}

	t.Run("image with registry", func(t *testing.T) {
		o, err := contain.NewBuildOutput("localhost:1234/test/foo:latest", h1)
		if err != nil {
			t.Error(err)
		}
		if len(o.Builds) != 1 {
			t.Errorf("%d builds", len(o.Builds))
		}
		if o.Builds[0].Tag != "localhost:1234/test/foo:latest@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f" {
			t.Errorf("ref %s", o.Builds[0].Tag)
		}
		if o.Builds[0].ImageName != "localhost:1234/test/foo" {
			t.Errorf("name %s", o.Builds[0].ImageName)
		}
		f, err := os.CreateTemp("", ".json")
		if err != nil {
			t.Error(err)
		}
		defer os.Remove(f.Name())
		o.WriteJSON(f)
		json, err := os.ReadFile(f.Name())
		if err != nil {
			t.Error(err)
		}
		if string(json) != "{\"builds\":[{\"imageName\":\"localhost:1234/test/foo\",\"tag\":\"localhost:1234/test/foo:latest@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f\"}]}" {
			t.Errorf("json %s", json)
		}
		http := o.Builds[0].Http()
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
		o, err := contain.NewBuildOutput("test/foo:a", h1)
		if err != nil {
			t.Error(err)
		}
		if len(o.Builds) != 1 {
			t.Errorf("%d builds", len(o.Builds))
		}
		if o.Builds[0].Tag != "test/foo:a@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f" {
			t.Errorf("ref %s", o.Builds[0].Tag)
		}
		if o.Builds[0].ImageName != "test/foo" {
			t.Errorf("name %s", o.Builds[0].ImageName)
		}
		http := o.Builds[0].Http()
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
		o, err := contain.NewBuildOutput("test/foo", h1)
		if err != nil {
			t.Error(err)
		}
		if len(o.Builds) != 1 {
			t.Errorf("%d builds", len(o.Builds))
		}
		if o.Builds[0].Tag != "test/foo@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f" {
			t.Errorf("ref %s", o.Builds[0].Tag)
		}
		if o.Builds[0].ImageName != "test/foo" {
			t.Errorf("name %s", o.Builds[0].ImageName)
		}
		http := o.Builds[0].Http()
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

	t.Run("bad input", func(t *testing.T) {
		var err error
		if _, err = contain.NewBuildOutput("", h1); err == nil {
			t.Error("Should have err'd on empty")
		}
		if _, err = contain.NewBuildOutput(":tag", h1); err == nil {
			t.Error("Should have err'd on tag only")
		}
		if _, err = contain.NewBuildOutput("test/foo@123", h1); err == nil {
			t.Error("Should have err'd on invalid digest")
		}
	})

}

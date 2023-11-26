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
			t.Errorf("tag %s", o.Builds[0].Tag)
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
	})

	t.Run("image with default registry", func(t *testing.T) {
		o, err := contain.NewBuildOutput("test/foo:latest", h1)
		if err != nil {
			t.Error(err)
		}
		if len(o.Builds) != 1 {
			t.Errorf("%d builds", len(o.Builds))
		}
		if o.Builds[0].Tag != "test/foo:latest@sha256:deadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33fdeadb33f" {
			t.Errorf("tag %s", o.Builds[0].Tag)
		}
		if o.Builds[0].ImageName != "test/foo" {
			t.Errorf("name %s", o.Builds[0].ImageName)
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
			t.Errorf("tag %s", o.Builds[0].Tag)
		}
		if o.Builds[0].ImageName != "test/foo" {
			t.Errorf("name %s", o.Builds[0].ImageName)
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

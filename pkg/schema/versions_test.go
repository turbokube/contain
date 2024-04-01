package schema_test

import (
	"testing"

	"github.com/turbokube/contain/pkg/schema"
)

func TestParse(t *testing.T) {

	cfg, err := schema.ParseConfig("../../test/localdir1/contain.yaml")
	if err != nil {
		t.Errorf("%v", err)
	}

	if cfg.Base != "docker.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966" {
		t.Errorf("Unexpected base: %s", cfg.Base)
	}
	if len(cfg.Layers) != 1 {
		t.Errorf("Unexpected layers: %d", len(cfg.Layers))
	}

}

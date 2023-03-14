package schema_test

import (
	"testing"

	"github.com/c9h-to/contain/pkg/schema"
)

func TestParse(t *testing.T) {

	cfg, err := schema.ParseConfig("../../test/localdir1/contain.yaml")
	if err != nil {
		t.Errorf("%v", err)
	}

	if cfg.Base != "docker.io/library/busybox" {
		t.Errorf("Unexpected base: %s", cfg.Base)
	}
	if len(cfg.Layers) != 1 {
		t.Errorf("Unexpected layers: %d", len(cfg.Layers))
	}

}

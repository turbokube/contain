package schema_test

import (
	"testing"

	"github.com/GoogleContainerTools/skaffold/v2/pkg/skaffold/tags"
	"github.com/turbokube/contain/pkg/schema"
	v1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestTemplateCompatibility(t *testing.T) {
	cfg1 := v1.ContainConfig{
		Base: "{{ .USER }}",
	}
	if err := tags.ApplyTemplates(&cfg1); err != nil {
		t.Errorf("ApplyTemplates: %v", err)
	}
	if cfg1.Base == "{{ .USER }}" {
		t.Errorf("template replacement failed: %v", cfg1.Base)
	}
}

func TestParse(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	t.Setenv("IMAGE", "localhost/a/b")
	cfg, err := schema.Parse([]byte(`
base: mirror.gcr.io/library/busybox
layers: []
tag: "{{.IMAGE}}"
`))
	if err != nil {
		t.Errorf("%v", err)
	}
	if cfg.Base != "mirror.gcr.io/library/busybox" {
		t.Errorf("Unexpected base: %s", cfg.Base)
	}
	if cfg.Tag != "localhost/a/b" {
		t.Errorf("Unexpected tag: %s", cfg.Tag)
	}

	// test actual file

	cfg, err = schema.ParseConfig("../../test/localdir1/contain.yaml")
	if err != nil {
		t.Errorf("%v", err)
	}

	if cfg.Base != "mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966" {
		t.Errorf("Unexpected base: %s", cfg.Base)
	}
	if len(cfg.Layers) != 1 {
		t.Errorf("Unexpected layers: %d", len(cfg.Layers))
	}

}

package schema_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/invopop/jsonschema"
	. "github.com/onsi/gomega"
	v1 "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestJsonschema(t *testing.T) {
	RegisterTestingT(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	t.Run("generate schema", func(t *testing.T) {
		s := jsonschema.Reflect(&v1.ContainConfig{})
		data, err := json.MarshalIndent(s, "", "  ")
		Expect(err).To(BeNil())
		f, err := os.Create("../..//jsonschema/config.json")
		Expect(err).To(BeNil())
		n, err := f.Write(data)
		Expect(err).To(BeNil())
		Expect(n > 0).To(BeTrue())
	})
}

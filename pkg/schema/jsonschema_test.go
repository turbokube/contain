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
		r := new(jsonschema.Reflector)
		err := r.AddGoComments("github.com/invopop/jsonschema", "./")
		Expect(err).To(BeNil())
		s := r.Reflect(&v1.ContainConfig{})
		data, err := json.MarshalIndent(s, "", "  ")
		Expect(err).To(BeNil())
		f, err := os.Create("../../jsonschema/config.json")
		Expect(err).To(BeNil())
		n, err := f.Write(data)
		Expect(err).To(BeNil())
		Expect(n > 0).To(BeTrue())

		// Guardrails on the LocalFile schema shape: pathPerPlatform is an
		// object with string values, and neither path nor pathPerPlatform
		// is individually required (either satisfies the config — enforced
		// at runtime by ValidateLayers, not by the JSON schema).
		var doc map[string]interface{}
		Expect(json.Unmarshal(data, &doc)).To(Succeed())
		defs := doc["$defs"].(map[string]interface{})
		localFile := defs["LocalFile"].(map[string]interface{})
		props := localFile["properties"].(map[string]interface{})
		ppp, ok := props["pathPerPlatform"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "LocalFile must declare a pathPerPlatform property")
		Expect(ppp["type"]).To(Equal("object"))
		Expect(ppp["additionalProperties"]).To(HaveKeyWithValue("type", "string"))
		_, hasRequired := localFile["required"]
		Expect(hasRequired).To(BeFalse(), "LocalFile must not hard-require path; either path or pathPerPlatform is accepted")
	})
}

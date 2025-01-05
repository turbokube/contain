package schema_test

import (
	"encoding/json"
	"fmt"
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
		if err != nil {
			zap.L().Fatal("marshal", zap.Error(err))
		}
		fmt.Println(string(data))
	})
}

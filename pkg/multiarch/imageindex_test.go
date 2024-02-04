package multiarch_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/multiarch"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestImageIndex(t *testing.T) {
	RegisterTestingT(t)

	logger := zaptest.NewLogger(t, zaptest.Level(zap.DebugLevel))
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := testcases.NewTestregistry(ctx)
	r.Start()

	index, err := multiarch.NewFromMultiArchBase(schema.ContainConfig{
		Base: fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest@sha256:c6dde17b43016c18361cf6b2db724b84312f074f9cb332438bc3908ac603f995", r.Host),
	}, &r.Config)
	if err != nil {
		t.Fatal(err)
	}

	prototype, err := index.GetPrototype()
	if err != nil {
		t.Error(err)
	}
	// the first item in the manifests array happens to be amd64
	Expect(prototype.DigestStr()).To(Equal("sha256:88b8e36da2fe3947b813bd52473319c3fb2e7637692ff4c499fa8bd878241852"))
	Expect(prototype.String()).To(Equal(fmt.Sprintf("%s/contain-test/baseimage-multiarch1@sha256:88b8e36da2fe3947b813bd52473319c3fb2e7637692ff4c499fa8bd878241852", r.Host)))

}

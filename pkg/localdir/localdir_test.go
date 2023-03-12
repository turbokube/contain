package localdir_test

import (
	"testing"

	"github.com/c9h-to/contain/pkg/localdir"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func debug(layer v1.Layer, t *testing.T) {
	// not implemented
}

func expectDigest(input localdir.Dir, digest string, t *testing.T) {
	result, err := localdir.FromFilesystem(input)
	if err != nil {
		t.Error(err)
	}
	d1, err := result.Digest()
	if err != nil {
		t.Error(err)
	}
	if d1.String() != digest {
		debug(result, t)
		t.Errorf("Unexpected digest: %s", d1.String())
	}
}

func TestFromFilesystemDir1(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	expectDigest(localdir.Dir{
		Path: "./testdata/dir1",
	}, "sha256:8df27a258c18cb9211f9a48177fb663b6db9b1e4a484d5c1736b0bdc7989a38a", t)

	expectDigest(localdir.Dir{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
	}, "sha256:0461765c4503fbbcec27a53b6b9db9f413b098f65f84f79df0f9585b5e2294f7", t)

}

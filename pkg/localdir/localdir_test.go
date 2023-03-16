package localdir_test

import (
	"testing"

	"github.com/c9h-to/contain/pkg/localdir"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
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
	}, "sha256:545dc99b3997be1f82cc1fc559ca9495e438eaf4d55d1827deb672cfc171504e", t)

	expectDigest(localdir.Dir{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
	}, "sha256:5135d234403e9b548686de3a65ed302923b15a662e7a0a202efc2ea7d81d89e6", t)

	ignoreA, err := patternmatcher.New([]string{"a.*"})
	if err != nil {
		t.Errorf("patternmatcher: %v", err)
	}
	expectDigest(localdir.Dir{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
		Ignore:        ignoreA,
	}, "sha256:fad4816a0e3821e9f23b6b4a9b2003d201ce17ad67ccb1b28734c0ed675dad7b", t)

	ignoreAll, err := patternmatcher.New([]string{"*"})
	if err != nil {
		t.Errorf("patternmatcher: %v", err)
	}
	result, err := localdir.FromFilesystem(localdir.Dir{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
		Ignore:        ignoreAll,
	})
	if err == nil {
		t.Errorf("Expected failure for localDir layer with no files")
	}
	if result != nil {
		t.Errorf("Expected no result when there's an error")
	}
}

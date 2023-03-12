package localdir

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
	"go.uber.org/zap"
)

type PathMapper func(string) string

type Dir struct {
	Path          string
	ContainerPath PathMapper
	Ignore        *patternmatcher.PatternMatcher
}

func NewPathMapperPrepend(prependDir string) PathMapper {
	if !strings.HasPrefix(prependDir, "/") {
		log.Fatalf("prependDir must have leading slash, got: %s", prependDir)
	}
	if strings.HasSuffix(prependDir, "/") {
		log.Fatalf("prependDir should be a path without trailing slash, got: %s", prependDir)
	}
	return func(original string) string {
		return fmt.Sprintf("%s/%s", prependDir, original)
	}
}

func NewPathMapperAsIs() PathMapper {
	return func(original string) string {
		return original
	}
}

func FromFilesystem(dir Dir) (v1.Layer, error) {

	if dir.Path == "" {
		return nil, fmt.Errorf("localDir must be specified (use . for CWD)")
	}

	if dir.ContainerPath == nil {
		dir.ContainerPath = NewPathMapperAsIs()
	}

	if dir.Ignore == nil {
		dir.Ignore, _ = patternmatcher.New([]string{})
	}

	filemap := make(map[string][]byte)

	fileSystem := os.DirFS(dir.Path)

	fs.WalkDir(fileSystem, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if d.Type().IsDir() {
			return nil
		}
		ignore, err := dir.Ignore.MatchesOrParentMatches(path)
		if err != nil {
			return err
		}
		if ignore {
			zap.L().Debug("ignored", zap.String("path", path))
			return nil
		}
		file, err := fs.ReadFile(fileSystem, path)
		if err != nil {
			return err
		}
		topath := dir.ContainerPath(path)
		filemap[topath] = file
		zap.L().Debug("added",
			zap.String("from", path),
			zap.String("to", topath),
			zap.Int("size", len(file)),
		)

		return nil
	})

	return crane.Layer(filemap)

}

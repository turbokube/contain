package localdir

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
	"go.uber.org/zap"
)

type PathMapper func(string) string

type Dir struct {
	Path          string
	ContainerPath PathMapper
	Ignore        *patternmatcher.PatternMatcher
	MaxFiles      int
	MaxSize       int
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

	bytesTotal := 0
	filemap := make(map[string][]byte)

	fileSystem := os.DirFS(dir.Path)

	err := fs.WalkDir(fileSystem, ".", func(path string, d fs.DirEntry, err error) error {
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
		if dir.MaxFiles > 0 && len(filemap) >= dir.MaxFiles {
			return fmt.Errorf("number of files exceeds max from layer config: %d", dir.MaxFiles)
		}
		file, err := fs.ReadFile(fileSystem, path)
		if err != nil {
			return err
		}
		bytesTotal = bytesTotal + len(file)
		if dir.MaxSize > 0 && bytesTotal > dir.MaxSize {
			return fmt.Errorf("accumulated file size %d exceeds max size from layer config: %d", bytesTotal, dir.MaxSize)
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

	if err != nil {
		zap.L().Error("layer buffer failed", zap.Int("files", len(filemap)), zap.Int("bytes", bytesTotal), zap.Error(err))
		return nil, err
	}
	zap.L().Info("layer buffer created", zap.Int("files", len(filemap)), zap.Int("bytes", bytesTotal))

	if len(filemap) == 0 {
		return nil, fmt.Errorf("dir resulted in empty layer: %v", dir)
	}

	return Layer(filemap)

}

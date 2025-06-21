package localdir

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

type PathMapper func(string) string

type From struct {
	isFile        bool
	Path          string
	ContainerPath PathMapper
	Ignore        *patternmatcher.PatternMatcher
	MaxFiles      int
	MaxSize       int
}

func NewFile() From {
	return From{
		isFile:   true,
		Ignore:   nil,
		MaxFiles: 1,
	}
}

func NewDir() From {
	return From{
		isFile: false,
	}
}

func NewPathMapperPrepend(prependDir string) PathMapper {
	if !strings.HasPrefix(prependDir, "/") {
		log.Fatalf("prependDir must have leading slash, got: %s", prependDir)
	}
	if strings.HasSuffix(prependDir, "/") {
		log.Fatalf("prependDir should be a path without trailing slash, got: %s", prependDir)
	}
	return func(original string) string {
		if original == "." {
			return prependDir
		}
		return fmt.Sprintf("%s/%s", prependDir, original)
	}
}

func NewPathMapperAsIs() PathMapper {
	return func(original string) string {
		return original
	}
}

func FromFilesystem(dir From, attributes schema.LayerAttributes) (v1.Layer, error) {
	// Use the new metadata-aware function for reproducible builds
	return FromFilesystemWithMetadata(dir, attributes)
}

// FromFilesystemWithMetadata creates a layer that preserves file metadata for reproducible builds
func FromFilesystemWithMetadata(dir From, attributes schema.LayerAttributes) (v1.Layer, error) {
	if dir.Path == "" {
		return nil, fmt.Errorf("path must be specified (use . for CWD)")
	}

	if dir.ContainerPath == nil {
		if dir.isFile {
			return nil, errors.New("localFile layer requires containerPath")
		}
		dir.ContainerPath = NewPathMapperAsIs()
	}

	if dir.Ignore == nil {
		dir.Ignore, _ = patternmatcher.New([]string{})
	}

	bytesTotal := 0
	var files []FileInfo
	var byteSource fs.FS

	// Directories we've seen, to ensure we add them to the tar
	seenDirs := make(map[string]bool)

	add := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			zap.L().Error("walk", zap.String("dir", dir.Path), zap.String("path", path), zap.Error(err))
			if path == "." && d == nil && !dir.isFile {
				zap.L().Info("To add a single file use a localFile layer instead of localDir", zap.String("path", dir.Path))
				return errors.New("localDir configured for what looks like a file: " + dir.Path)
			}
			return err
		}

		ignore, err := dir.Ignore.MatchesOrParentMatches(path)
		if err != nil {
			return err
		}
		if ignore {
			zap.L().Debug("ignored", zap.String("path", path))
			return nil
		}

		topath := dir.ContainerPath(path)

		// Handle directory
		if d != nil && d.Type().IsDir() {
			if !seenDirs[topath] {
				info, err := d.Info()
				if err != nil {
					return err
				}
				files = append(files, FileInfo{
					Path:    topath,
					Content: nil,
					Mode:    info.Mode(),
					IsDir:   true,
					IsSymlink: false,
				})
				seenDirs[topath] = true
			}
			return nil
		}

		// Check file limits
		if dir.MaxFiles > 0 && len(files) >= dir.MaxFiles {
			return fmt.Errorf("number of files exceeds max from layer config: %d", dir.MaxFiles)
		}

		// Get file info for metadata
		var fileInfo os.FileInfo
		if d != nil {
			var err error
			fileInfo, err = d.Info()
			if err != nil {
				return err
			}
		}

		// Handle symlinks
		if fileInfo != nil && fileInfo.Mode()&os.ModeSymlink != 0 {
			// For symlinks, we need to read the link target
			linkTarget, err := os.Readlink(filepath.Join(dir.Path, path))
			if err != nil {
				return err
			}

			// Only preserve symlinks that point within the same source tree
			if isWithinSourceTree(linkTarget, dir.Path) {
				files = append(files, FileInfo{
					Path:       topath,
					Content:    nil,
					Mode:       fileInfo.Mode(),
					IsDir:      false,
					IsSymlink:  true,
					LinkTarget: linkTarget,
				})
				zap.L().Debug("added symlink",
					zap.String("from", path),
					zap.String("to", topath),
					zap.String("target", linkTarget),
				)
			} else {
				zap.L().Warn("skipping symlink pointing outside source tree",
					zap.String("path", path),
					zap.String("target", linkTarget),
				)
			}
			return nil
		}

		// Handle regular files
		file, err := fs.ReadFile(byteSource, path)
		if err != nil {
			return err
		}
		bytesTotal = bytesTotal + len(file)
		if dir.MaxSize > 0 && bytesTotal > dir.MaxSize {
			return fmt.Errorf("accumulated file size %d exceeds max size from layer config: %d", bytesTotal, dir.MaxSize)
		}

		mode := os.FileMode(0644)
		if fileInfo != nil {
			mode = fileInfo.Mode()
		}

		files = append(files, FileInfo{
			Path:      topath,
			Content:   file,
			Mode:      mode,
			IsDir:     false,
			IsSymlink: false,
		})

		zap.L().Debug("added file",
			zap.String("from", path),
			zap.String("to", topath),
			zap.Int("size", len(file)),
			zap.String("mode", mode.String()),
		)

		return nil
	}

	var err error
	if !dir.isFile {
		byteSource = os.DirFS(dir.Path)
		err = fs.WalkDir(byteSource, ".", add)
	} else {
		byteSource = NewSingleFileFS(dir.Path)
		err = add(".", nil, nil)
	}

	if err != nil {
		zap.L().Error("layer buffer failed", zap.Int("files", len(files)), zap.Int("bytes", bytesTotal), zap.Error(err))
		return nil, err
	}
	zap.L().Info("layer buffer created", zap.Int("files", len(files)), zap.Int("bytes", bytesTotal))

	if len(files) == 0 {
		return nil, fmt.Errorf("dir resulted in empty layer: %v", dir)
	}

	return LayerFromFiles(files, attributes)
}

// isWithinSourceTree checks if a symlink target points within the source tree
func isWithinSourceTree(linkTarget, sourcePath string) bool {
	// If the link target is absolute, it's outside our source tree
	if filepath.IsAbs(linkTarget) {
		return false
	}

	// For relative paths, we accept them (simple heuristic)
	// TODO: Could be enhanced to resolve and check if it actually points within the tree
	return true
}

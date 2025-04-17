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
	filemap := make(map[string][]byte)
	// Track directories separately
	dirmap := make(map[string]bool)
	// Track symlinks separately
	symlinkMap := make(map[string]bool)
	// Track source paths for container paths
	sourcePathMap := make(map[string]string)
	// Track original file modes from filesystem
	modeMap := make(map[string]int64)
	var byteSource fs.FS

	add := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			zap.L().Error("walk", zap.String("dir", dir.Path), zap.String("path", path), zap.Error(err))
			if path == "." && d == nil && !dir.isFile {
				zap.L().Info("To add a single file use a localFile layer istead of localDir", zap.String("path", dir.Path))
				return errors.New("localDir configured for what looks like a file: " + dir.Path)
			}
			return err
		}
		if d != nil {
			// Handle different file types
			fileType := d.Type()
			topath := dir.ContainerPath(path)
			
			// Handle directories
			if fileType.IsDir() {
				// Track directories to add them to the tar with proper permissions
				dirmap[topath] = true
				
				// Capture the original file mode from the filesystem
				absPath := filepath.Join(dir.Path, path)
				if info, err := os.Stat(absPath); err == nil {
					modeMap[topath] = int64(info.Mode() & 0777) // Only preserve permission bits
				}
				
				zap.L().Debug("added directory",
					zap.String("from", path),
					zap.String("to", topath),
				)
				return nil
			}
			
			// Handle symlinks
			if fileType&fs.ModeSymlink != 0 {
				// For symlinks, we need to get the target using os.Readlink
				// since fs.FS doesn't provide direct access to symlink targets
				absPath := filepath.Join(dir.Path, path)
				target, err := os.Readlink(absPath)
				if err != nil {
					return fmt.Errorf("failed to read symlink %s: %w", path, err)
				}
				
				// Capture the original file mode from the filesystem
				if info, err := os.Lstat(absPath); err == nil {
					modeMap[topath] = int64(info.Mode() & 0777) // Only preserve permission bits
				}
				
				// For absolute symlinks, we need to convert them to relative paths
				// that will work correctly in the container context
				if filepath.IsAbs(target) {
					// For absolute symlinks within the source directory, convert to relative paths
					// that will work in the container
					
					// First, get the base filename from the target
					baseName := filepath.Base(target)
					
					// For symlinks to files, just use the filename
					// This works because we're flattening the directory structure in the container
					target = baseName
					
					// For directory symlinks, we need to check if it points to a directory we've added
					dirPath := filepath.Dir(target)
					if dirPath != "/" {
						// Check if this is a directory we've already processed
						for containerDirPath := range dirmap {
							if containerDirPath != "." && strings.HasSuffix(target, containerDirPath) {
								target = containerDirPath
								break
							}
						}
					}
					
					zap.L().Debug("converted absolute symlink to relative",
						zap.String("path", path),
						zap.String("original", target),
						zap.String("converted", target),
					)
				}
				
				// Store the symlink in a special map
				filemap[topath] = []byte(target)
				
				// Mark this as a symlink in a separate map
				symlinkMap[topath] = true
				
				// Record the source path for this container path
				sourcePathMap[topath] = path
				
				zap.L().Debug("added symlink",
					zap.String("from", path),
					zap.String("to", topath),
					zap.String("target", target),
				)
				return nil
			}
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
		file, err := fs.ReadFile(byteSource, path)
		if err != nil {
			return err
		}
		bytesTotal = bytesTotal + len(file)
		if dir.MaxSize > 0 && bytesTotal > dir.MaxSize {
			return fmt.Errorf("accumulated file size %d exceeds max size from layer config: %d", bytesTotal, dir.MaxSize)
		}
		topath := dir.ContainerPath(path)
		filemap[topath] = file
		// Record the source path for this container path
		sourcePathMap[topath] = path
		
		// Capture the original file mode from the filesystem
		absPath := filepath.Join(dir.Path, path)
		if info, err := os.Stat(absPath); err == nil {
			modeMap[topath] = int64(info.Mode() & 0777) // Only preserve permission bits
		}
		
		zap.L().Debug("added",
			zap.String("from", path),
			zap.String("to", topath),
			zap.Int("size", len(file)),
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
		zap.L().Error("layer buffer failed", zap.Int("files", len(filemap)), zap.Int("bytes", bytesTotal), zap.Error(err))
		return nil, err
	}

	// Create a summary of the layer contents for DEBUG level
	// Find the largest file
	var largestFilePath string
	var largestFileSize int
	for path, content := range filemap {
		if largestFilePath == "" || len(content) > largestFileSize {
			largestFilePath = path
			largestFileSize = len(content)
		}
	}

	// Count files per directory
	dirFileCounts := make(map[string]int)
	for path := range filemap {
		// Get parent directory
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash > 0 {
			parentDir := path[:lastSlash]
			dirFileCounts[parentDir]++
		} else {
			// Root files
			dirFileCounts["/"]++
		}
	}

	// Find directory with most files
	var biggestDirPath string
	var biggestDirCount int
	for path, count := range dirFileCounts {
		if biggestDirPath == "" || count > biggestDirCount {
			biggestDirPath = path
			biggestDirCount = count
		}
	}

	// Prepare log fields
	logFields := []zap.Field{
		zap.Int("files", len(filemap)),
		zap.Int("bytes", bytesTotal),
		zap.Int("dirs", len(dirmap)),
	}

	// Always include largest_file when files are found
	if largestFilePath != "" {
		// Get the source path for the largest file
		sourcePath, ok := sourcePathMap[largestFilePath]
		if !ok {
			sourcePath = "unknown"
		}
		largestFileStr := fmt.Sprintf("%s -> %s (%d bytes)", sourcePath, largestFilePath, largestFileSize)
		logFields = append(logFields, zap.String("largest_file", largestFileStr))
	}

	// Include biggest_dir when there are any directories with files
	if len(dirFileCounts) > 0 && biggestDirPath != "" {
		// For directories, we need to find a file in that directory to get the source mapping
		var sourceDirPath string
		for containerPath, sourcePath := range sourcePathMap {
			// Check if this file is in the biggest directory
			if strings.HasPrefix(containerPath, biggestDirPath+"/") {
				// Extract the source directory from the source file path
				lastSlash := strings.LastIndex(sourcePath, "/")
				if lastSlash > 0 {
					sourceDirPath = sourcePath[:lastSlash]
				} else {
					sourceDirPath = "."
				}
				break
			}
		}
		// If we couldn't find a source directory, use a placeholder
		if sourceDirPath == "" {
			sourceDirPath = "unknown"
		}
		biggestDirStr := fmt.Sprintf("%s -> %s (%d files)", sourceDirPath, biggestDirPath, biggestDirCount)
		logFields = append(logFields, zap.String("biggest_dir", biggestDirStr))
	}

	zap.L().Info("layer buffer created", logFields...)

	if len(filemap) == 0 {
		return nil, fmt.Errorf("dir resulted in empty layer: %v", dir)
	}

	return Layer(filemap, dirmap, symlinkMap, modeMap, attributes)

}

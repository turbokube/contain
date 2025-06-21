package localdir

// https://github.com/solsson/go-containerregistry/compare/filemap-mode?expand=1

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"sort"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

const (
	defaultFileMode = int64(0644)
	defaultDirMode  = int64(0755)
	executableMask  = int64(0111) // mask to check executable bits
)

var (
	// SOURCE_DATE_EPOCH is set to Unix epoch for reproducible builds
	SOURCE_DATE_EPOCH = time.Unix(0, 0)
)

// FileInfo represents file metadata for tar layer creation
type FileInfo struct {
	Path     string
	Content  []byte
	Mode     os.FileMode
	IsDir    bool
	IsSymlink bool
	LinkTarget string
}

// Layer creates a layer from a single file map. These layers are reproducible and consistent.
// A filemap is a path -> file content map representing a file system.
func Layer(filemap map[string][]byte, attributes schema.LayerAttributes) (v1.Layer, error) {
	// Convert filemap to FileInfo for backward compatibility
	var files []FileInfo
	for path, content := range filemap {
		files = append(files, FileInfo{
			Path:    path,
			Content: content,
			Mode:    0644, // default file mode
			IsDir:   false,
			IsSymlink: false,
		})
	}
	return LayerFromFiles(files, attributes)
}

// LayerFromFiles creates a layer from file metadata with proper mode and timestamp handling
func LayerFromFiles(files []FileInfo, attributes schema.LayerAttributes) (v1.Layer, error) {
	b := &bytes.Buffer{}
	w := tar.NewWriter(b)

	// Sort by path for reproducible order
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	for _, file := range files {
		mode := calculateFileMode(file, attributes)
		var typeflag byte = tar.TypeReg
		
		if file.IsSymlink {
			typeflag = tar.TypeSymlink
		} else if file.IsDir {
			typeflag = tar.TypeDir
		}

		header := &tar.Header{
			Name:     file.Path,
			Mode:     mode,
			Uid:      int(attributes.Uid),
			Gid:      int(attributes.Gid),
			ModTime:  SOURCE_DATE_EPOCH,
			Typeflag: typeflag,
		}

		if file.IsSymlink {
			header.Linkname = file.LinkTarget
			header.Size = 0
		} else if file.IsDir {
			header.Size = 0
		} else {
			header.Size = int64(len(file.Content))
		}

		if err := w.WriteHeader(header); err != nil {
			return nil, err
		}

		if !file.IsDir && !file.IsSymlink {
			if _, err := w.Write(file.Content); err != nil {
				return nil, err
			}
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	// Return a new copy of the buffer each time it's opened.
	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBuffer(b.Bytes())), nil
	})
}

// calculateFileMode determines the appropriate file mode based on requirements:
// - Use 0644 for files and 0755 for directories by default
// - Preserve executable bit from source files
// - Allow override via layer attributes
func calculateFileMode(file FileInfo, attributes schema.LayerAttributes) int64 {
	var mode int64

	if file.IsDir {
		mode = defaultDirMode
		if attributes.DirMode != 0 {
			mode = int64(attributes.DirMode)
		}
	} else {
		mode = defaultFileMode
		if attributes.FileMode != 0 {
			mode = int64(attributes.FileMode)
		} else {
			// Preserve executable bit from source
			if file.Mode&0111 != 0 {
				mode = mode | executableMask
			}
		}
	}

	return mode
}

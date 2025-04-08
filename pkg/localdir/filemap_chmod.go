package localdir

// https://github.com/solsson/go-containerregistry/compare/filemap-mode?expand=1

import (
	"archive/tar"
	"bytes"
	"io"
	"sort"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

const (
	defaultFileMode = int64(0644)
)

// Layer creates a layer from a single file map and directory map. These layers are reproducible and consistent.
// A filemap is a path -> file content map representing a file system.
// A dirmap is a path -> bool map representing directories to be created with proper permissions.
func Layer(filemap map[string][]byte, dirmap map[string]bool, attributes schema.LayerAttributes) (v1.Layer, error) {
	b := &bytes.Buffer{}
	w := tar.NewWriter(b)

	fn := []string{}
	for f := range filemap {
		fn = append(fn, f)
	}
	sort.Strings(fn)

	// First add directories with proper permissions
	dn := []string{}
	for d := range dirmap {
		dn = append(dn, d)
	}
	sort.Strings(dn)

	for _, d := range dn {
		// Use directory mode (add execute bits to match standard directory permissions)
		mode := int64(0755) // Default directory mode
		if attributes.DirMode != 0 {
			// Use the specific directory mode if provided
			mode = int64(attributes.DirMode)
		} else if attributes.FileMode != 0 {
			// If dirMode is not specified, fall back to mode (FileMode)
			mode = int64(attributes.FileMode)
		}
		if err := w.WriteHeader(&tar.Header{
			Name:     d,
			Size:     0, // Directories have zero size
			Uid:      int(attributes.Uid),
			Gid:      int(attributes.Gid),
			Mode:     mode,
			Typeflag: tar.TypeDir,
		}); err != nil {
			return nil, err
		}
	}

	// Then add files
	for _, f := range fn {
		c := filemap[f]
		mode := defaultFileMode
		if attributes.FileMode != 0 {
			mode = int64(attributes.FileMode)
		}
		if err := w.WriteHeader(&tar.Header{
			Name:     f,
			Size:     int64(len(c)),
			Uid:      int(attributes.Uid),
			Gid:      int(attributes.Gid),
			Mode:     mode,
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, err
		}
		if _, err := w.Write(c); err != nil {
			return nil, err
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

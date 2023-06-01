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

// Layer creates a layer from a single file map. These layers are reproducible and consistent.
// A filemap is a path -> file content map representing a file system.
func Layer(filemap map[string][]byte, attributes schema.LayerAttributes) (v1.Layer, error) {
	b := &bytes.Buffer{}
	w := tar.NewWriter(b)

	fn := []string{}
	for f := range filemap {
		fn = append(fn, f)
	}
	sort.Strings(fn)

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

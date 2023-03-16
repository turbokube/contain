package localdir

// https://github.com/solsson/go-containerregistry/compare/filemap-mode?expand=1

import (
	"archive/tar"
	"bytes"
	"io"
	"sort"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Layer creates a layer from a single file map. These layers are reproducible and consistent.
// A filemap is a path -> file content map representing a file system.
func Layer(filemap map[string][]byte) (v1.Layer, error) {
	b := &bytes.Buffer{}
	w := tar.NewWriter(b)

	fn := []string{}
	for f := range filemap {
		fn = append(fn, f)
	}
	sort.Strings(fn)

	for _, f := range fn {
		c := filemap[f]
		if err := w.WriteHeader(&tar.Header{
			Name: f,
			Size: int64(len(c)),
			// from pkg/legacy/tarball writeTarEntry
			Mode:     0644,
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

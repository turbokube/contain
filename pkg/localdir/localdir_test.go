package localdir_test

import (
	"archive/tar"
	"io"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/patternmatcher"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/localdir"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func debug(layer v1.Layer, t *testing.T) {
	// not implemented
}

func expectDigest(input localdir.From, digest string, t *testing.T) {
	expectDigestWithAttributes(schema.LayerAttributes{}, input, digest, t)
}

func expectDigestWithAttributes(
	a schema.LayerAttributes,
	input localdir.From,
	digest string,
	t *testing.T,
) {
	result, err := localdir.FromFilesystem(input, a)
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

	expectDigest(localdir.From{
		Path: "./testdata/dir1",
	}, "sha256:5c116b43715d4cb103a472dcc384f4d0e8fb92e79e38c194178b0b7013a49be3", t)

	expectDigest(localdir.From{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
	}, "sha256:39af1efac071289a4ca4c163b9c93083eed24afa07721984e5d7b6ab36042645", t)

	ignoreA, err := patternmatcher.New([]string{"a.*"})
	if err != nil {
		t.Errorf("patternmatcher: %v", err)
	}
	expectDigest(localdir.From{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
		Ignore:        ignoreA,
	}, "sha256:befccdb1423b50fdf5691e8126c80b875d449340c31ef5efd9a97cd1a0ee707c", t)

	ignoreAll, err := patternmatcher.New([]string{"*"})
	if err != nil {
		t.Errorf("patternmatcher: %v", err)
	}
	result, err := localdir.FromFilesystem(localdir.From{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
		Ignore:        ignoreAll,
	}, schema.LayerAttributes{})
	if err == nil {
		t.Errorf("Expected failure for localDir layer with no files")
	}
	if result != nil {
		t.Errorf("Expected no result when there's an error")
	}

	expectDigest(localdir.From{
		Path: "./testdata/dir2",
	}, "sha256:85ce5400f21fc875bcf575243ae29db958d07699b07eb6d00f532e9e1d806bda", t)

	expectDigestWithAttributes(schema.LayerAttributes{FileMode: 0755}, localdir.From{
		Path: "./testdata/dir2",
	}, "sha256:7f7f123e57c33d58d0efc1d1973852b4e981eece16209a4eab939138ea711140", t)

	expectDigestWithAttributes(schema.LayerAttributes{Uid: 65532}, localdir.From{
		Path: "./testdata/dir2",
	}, "sha256:b879074782a944a7699c32cefc4d76ec99c480f953735dd33166e4083de928bc", t)

	expectDigestWithAttributes(schema.LayerAttributes{Gid: 65534}, localdir.From{
		Path: "./testdata/dir2",
	}, "sha256:d732c7242056913aaa8195a11d009cdceb843058c616d8dec4659927e6209984", t)

}

func TestPathMapperAsIs(t *testing.T) {
	RegisterTestingT(t)
	mapper := localdir.NewPathMapperAsIs()
	Expect(mapper("t")).To(Equal("t"))
}

func TestNewPathMapperPrepend(t *testing.T) {
	RegisterTestingT(t)
	mapper := localdir.NewPathMapperPrepend("/prep")
	Expect(mapper("t")).To(Equal("/prep/t"))
	Expect(mapper(".")).To(Equal("/prep"))
}

func TestSymlinksPreserved(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	RegisterTestingT(t)

	// This test verifies that symlinks are preserved in localDir layers

	// Create a layer from the test directory with symlinks
	layer, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/symlinks",
	}, schema.LayerAttributes{})
	Expect(err).NotTo(HaveOccurred())

	// Extract the layer to verify symlinks are preserved
	reader, err := layer.Uncompressed()
	Expect(err).NotTo(HaveOccurred())
	defer reader.Close()

	// Parse the tar archive
	tr := tar.NewReader(reader)
	
	// Maps to track what we find in the tar
	foundFiles := make(map[string]bool)
	foundSymlinks := make(map[string]string) // path -> linkTarget
	foundDirs := make(map[string]bool)

	// Read all entries in the tar
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())

		t.Logf("Found entry: %s, type: %d", header.Name, header.Typeflag)

		// Record the entry based on its type
		switch header.Typeflag {
		case tar.TypeReg: // Regular file
			foundFiles[header.Name] = true
		case tar.TypeSymlink: // Symlink
			foundSymlinks[header.Name] = header.Linkname
		case tar.TypeDir: // Directory
			foundDirs[header.Name] = true
		}
	}

	// Verify that we found the expected files
	Expect(foundFiles).To(HaveKey("testfile.txt"))
	Expect(foundFiles).To(HaveKey("dir1/dirfile.txt"))

	// Verify that we found the expected symlinks with correct targets
	// Relative symlinks should preserve their relative paths
	Expect(foundSymlinks).To(HaveKey("relative-symlink.txt"))
	Expect(foundSymlinks["relative-symlink.txt"]).To(Equal("testfile.txt"))

	Expect(foundSymlinks).To(HaveKey("relative-dir-symlink"))
	Expect(foundSymlinks["relative-dir-symlink"]).To(Equal("dir1"))

	// Absolute symlinks should be converted to relative paths in the layer
	Expect(foundSymlinks).To(HaveKey("absolute-symlink.txt"))
	Expect(foundSymlinks["absolute-symlink.txt"]).To(Equal("testfile.txt"))

	Expect(foundSymlinks).To(HaveKey("absolute-dir-symlink"))
	Expect(foundSymlinks["absolute-dir-symlink"]).To(Equal("dir1"))
}

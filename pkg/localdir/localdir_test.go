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
	// Extract and examine the tar content to debug layer contents
	rc, err := layer.Uncompressed()
	if err != nil {
		t.Errorf("Failed to get uncompressed layer: %v", err)
		return
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	t.Log("Layer contents:")
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("Error reading tar: %v", err)
			break
		}
		t.Logf("  %s: mode=%o, uid=%d, gid=%d, size=%d, type=%c, modtime=%s",
			header.Name, header.Mode, header.Uid, header.Gid,
			header.Size, header.Typeflag, header.ModTime.UTC())
		if header.Typeflag == tar.TypeSymlink {
			t.Logf("    -> symlink target: %s", header.Linkname)
		}
	}
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

	// Updated expectations for reproducible builds
	expectDigest(localdir.From{
		Path: "./testdata/dir1",
	}, "sha256:1e045563454a1b6dad232ebd8a466e8debf0d1d9c49c807d3e13e25a6dd3946b", t)

	expectDigest(localdir.From{
		Path:          "./testdata/dir1",
		ContainerPath: localdir.NewPathMapperPrepend("/app"),
	}, "sha256:fe7dfab2d0a720ae7271d16ff803544d01bfa08ed87c613383a2664b45e88125", t)

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

func TestReproducibleBuilds(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	// Test that timestamps are set to SOURCE_DATE_EPOCH
	layer, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/reproducible",
	}, schema.LayerAttributes{})
	if err != nil {
		t.Fatal(err)
	}

	// Verify layer structure includes directories and proper timestamps
	rc, err := layer.Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	entries := make(map[string]*tar.Header)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = header
	}

	// Check that we have the expected entries
	expectedEntries := []string{".", "normal.txt", "script.sh", "symlink.txt"}
	for _, expected := range expectedEntries {
		if _, exists := entries[expected]; !exists {
			t.Errorf("Expected entry %s not found in layer", expected)
		}
	}

	// Verify timestamps are set to SOURCE_DATE_EPOCH
	for name, header := range entries {
		if !header.ModTime.Equal(localdir.SOURCE_DATE_EPOCH) {
			t.Errorf("Entry %s has incorrect timestamp %v, expected %v",
				name, header.ModTime, localdir.SOURCE_DATE_EPOCH)
		}
	}

	// Verify file modes
	if entries["normal.txt"].Mode != 0644 {
		t.Errorf("normal.txt should have mode 0644, got %o", entries["normal.txt"].Mode)
	}

	// script.sh should preserve executable bit
	if entries["script.sh"].Mode != 0755 {
		t.Errorf("script.sh should have mode 0755 (preserving executable bit), got %o", entries["script.sh"].Mode)
	}

	// symlink should be preserved
	if entries["symlink.txt"].Typeflag != tar.TypeSymlink {
		t.Errorf("symlink.txt should be a symlink, got typeflag %c", entries["symlink.txt"].Typeflag)
	}
	if entries["symlink.txt"].Linkname != "normal.txt" {
		t.Errorf("symlink.txt should link to normal.txt, got %s", entries["symlink.txt"].Linkname)
	}

	// Directory should have proper mode
	if entries["."].Mode != 0755 {
		t.Errorf("Directory should have mode 0755, got %o", entries["."].Mode)
	}
}

func TestModeOverrides(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	// Test file mode override
	layer, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/reproducible",
	}, schema.LayerAttributes{
		FileMode: 0600,
		DirMode:  0700,
	})
	if err != nil {
		t.Fatal(err)
	}

	rc, err := layer.Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	entries := make(map[string]*tar.Header)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = header
	}

	// Verify overridden modes
	if entries["normal.txt"].Mode != 0600 {
		t.Errorf("normal.txt should have overridden mode 0600, got %o", entries["normal.txt"].Mode)
	}

	if entries["."].Mode != 0700 {
		t.Errorf("Directory should have overridden mode 0700, got %o", entries["."].Mode)
	}
}

func TestReproducibleBuildsDeterministic(t *testing.T) {
	logger := zap.NewNop()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	// Create the same layer twice and verify identical digests
	layer1, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/reproducible",
	}, schema.LayerAttributes{})
	if err != nil {
		t.Fatal(err)
	}

	layer2, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/reproducible",
	}, schema.LayerAttributes{})
	if err != nil {
		t.Fatal(err)
	}

	digest1, err := layer1.Digest()
	if err != nil {
		t.Fatal(err)
	}

	digest2, err := layer2.Digest()
	if err != nil {
		t.Fatal(err)
	}

	if digest1.String() != digest2.String() {
		t.Errorf("Layers should have identical digests for reproducible builds, got %s vs %s",
			digest1.String(), digest2.String())
	}

	t.Logf("Reproducible digest: %s", digest1.String())
}

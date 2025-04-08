package localdir_test

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
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
	// Maps to track file modes
	fileModes := make(map[string]int64)
	symlinkModes := make(map[string]int64)
	dirModes := make(map[string]int64)

	// Read all entries in the tar
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())

		t.Logf("Found entry: %s, type: %d, mode: %o", header.Name, header.Typeflag, header.Mode)

		// Record the entry based on its type
		switch header.Typeflag {
		case tar.TypeReg: // Regular file
			foundFiles[header.Name] = true
			fileModes[header.Name] = header.Mode
		case tar.TypeSymlink: // Symlink
			foundSymlinks[header.Name] = header.Linkname
			symlinkModes[header.Name] = header.Mode
		case tar.TypeDir: // Directory
			foundDirs[header.Name] = true
			dirModes[header.Name] = header.Mode
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

	// Verify file modes
	// Since we didn't specify any modes in the attributes, we should get the default modes
	// Default file mode is 0644
	Expect(fileModes["testfile.txt"]).To(Equal(int64(0644)))
	Expect(fileModes["dir1/dirfile.txt"]).To(Equal(int64(0644)))

	// Default symlink mode is 0644
	Expect(symlinkModes["relative-symlink.txt"]).To(Equal(int64(0644)))
	Expect(symlinkModes["absolute-symlink.txt"]).To(Equal(int64(0644)))
	Expect(symlinkModes["relative-dir-symlink"]).To(Equal(int64(0644)))
	Expect(symlinkModes["absolute-dir-symlink"]).To(Equal(int64(0644)))

	// Default directory mode is 0755
	Expect(dirModes["dir1"]).To(Equal(int64(0755)))
}

// TestFileModePreservation tests whether file modes from the filesystem are preserved
func TestFileModePreservation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	RegisterTestingT(t)

	// Create a temporary directory for test files with specific permissions
	tempDir, err := os.MkdirTemp("", "filemodetest")
	Expect(err).NotTo(HaveOccurred())
	defer os.RemoveAll(tempDir)

	// Create a regular file with non-default permissions (0640)
	regularFilePath := filepath.Join(tempDir, "regular.txt")
	err = os.WriteFile(regularFilePath, []byte("regular file content"), 0640)
	Expect(err).NotTo(HaveOccurred())

	// Create an executable file with executable permissions (0755)
	execFilePath := filepath.Join(tempDir, "exec.sh")
	err = os.WriteFile(execFilePath, []byte("#!/bin/sh\necho 'Hello'"), 0755)
	Expect(err).NotTo(HaveOccurred())

	// Create a subdirectory with non-default permissions (0750)
	subdirPath := filepath.Join(tempDir, "subdir")
	err = os.Mkdir(subdirPath, 0750)
	Expect(err).NotTo(HaveOccurred())

	// Create a file in the subdirectory
	subdirFilePath := filepath.Join(subdirPath, "subfile.txt")
	err = os.WriteFile(subdirFilePath, []byte("subdir file content"), 0640)
	Expect(err).NotTo(HaveOccurred())

	// Create a symlink to the regular file
	symlinkPath := filepath.Join(tempDir, "symlink.txt")
	err = os.Symlink(regularFilePath, symlinkPath)
	Expect(err).NotTo(HaveOccurred())

	// Verify the actual permissions on the filesystem
	regularInfo, err := os.Stat(regularFilePath)
	Expect(err).NotTo(HaveOccurred())
	t.Logf("Filesystem mode for regular.txt: %o", regularInfo.Mode().Perm())

	execInfo, err := os.Stat(execFilePath)
	Expect(err).NotTo(HaveOccurred())
	t.Logf("Filesystem mode for exec.sh: %o", execInfo.Mode().Perm())

	subdirInfo, err := os.Stat(subdirPath)
	Expect(err).NotTo(HaveOccurred())
	t.Logf("Filesystem mode for subdir: %o", subdirInfo.Mode().Perm())

	// Create a layer from the temp directory
	layer, err := localdir.FromFilesystem(localdir.From{
		Path: tempDir,
	}, schema.LayerAttributes{})
	Expect(err).NotTo(HaveOccurred())

	// Extract the layer to check if file modes are preserved
	reader, err := layer.Uncompressed()
	Expect(err).NotTo(HaveOccurred())
	defer reader.Close()

	// Parse the tar archive
	tr := tar.NewReader(reader)
	
	// Maps to track file modes
	modes := make(map[string]int64)

	// Read all entries in the tar
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())

		t.Logf("Layer entry: %s, type: %d, mode: %o", header.Name, header.Typeflag, header.Mode)
		modes[header.Name] = header.Mode
	}

	// Check if file modes in the layer match the filesystem
	// With the current implementation, they should NOT match because modes are not preserved
	// Instead, default modes are used: 0644 for files, 0755 for directories
	
	// Regular file should have default mode 0644, not the original 0640
	Expect(modes["regular.txt"]).To(Equal(int64(0644)), "Regular file should have default mode 0644")
	
	// Executable file should have default mode 0644, not the original 0755
	Expect(modes["exec.sh"]).To(Equal(int64(0644)), "Executable file should have default mode 0644")
	
	// Directory should have default mode 0755, not the original 0750
	Expect(modes["subdir"]).To(Equal(int64(0755)), "Directory should have default mode 0755")
	
	// Symlink should have default mode 0644
	Expect(modes["symlink.txt"]).To(Equal(int64(0644)), "Symlink should have default mode 0644")

	// Note: If file mode preservation is implemented in the future, these tests would need to be updated
	// to expect the original modes from the filesystem instead of the default modes.
}

// TestFileModeHandling tests how file modes are handled in different scenarios
func TestFileModeHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	RegisterTestingT(t)

	// Helper function to extract and check file modes in a layer
	extractAndCheckModes := func(layer v1.Layer, description string) map[string]int64 {
		// Extract the layer to verify file modes
		reader, err := layer.Uncompressed()
		Expect(err).NotTo(HaveOccurred(), "Failed to get uncompressed reader for %s", description)
		defer reader.Close()

		// Parse the tar archive
		tr := tar.NewReader(reader)
		
		// Map to track file modes
		modes := make(map[string]int64)

		// Read all entries in the tar
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			Expect(err).NotTo(HaveOccurred())

			t.Logf("%s - Found entry: %s, type: %d, mode: %o", 
				description, header.Name, header.Typeflag, header.Mode)

			// Record the mode for this entry
			modes[header.Name] = header.Mode
		}

		return modes
	}

	// Test Case 1: Default modes (no mode specified in attributes)
	layer1, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/symlinks",
	}, schema.LayerAttributes{})
	Expect(err).NotTo(HaveOccurred())

	modes1 := extractAndCheckModes(layer1, "Default modes")
	
	// Verify default modes are applied
	// Regular files should have mode 0644
	Expect(modes1["testfile.txt"]).To(Equal(int64(0644)), "Default file mode should be 0644")
	Expect(modes1["dir1/dirfile.txt"]).To(Equal(int64(0644)), "Default file mode should be 0644")
	
	// Symlinks should have mode 0644
	Expect(modes1["relative-symlink.txt"]).To(Equal(int64(0644)), "Default symlink mode should be 0644")
	Expect(modes1["absolute-symlink.txt"]).To(Equal(int64(0644)), "Default symlink mode should be 0644")
	
	// Directories should have mode 0755
	Expect(modes1["dir1"]).To(Equal(int64(0755)), "Default directory mode should be 0755")

	// Test Case 2: Explicit FileMode in attributes
	layer2, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/symlinks",
	}, schema.LayerAttributes{FileMode: 0600})
	Expect(err).NotTo(HaveOccurred())

	modes2 := extractAndCheckModes(layer2, "Explicit FileMode")
	
	// Verify explicit FileMode is applied to files and symlinks
	// Regular files should have the specified mode
	Expect(modes2["testfile.txt"]).To(Equal(int64(0600)), "File mode should match explicit FileMode")
	Expect(modes2["dir1/dirfile.txt"]).To(Equal(int64(0600)), "File mode should match explicit FileMode")
	
	// Symlinks should have the specified mode
	Expect(modes2["relative-symlink.txt"]).To(Equal(int64(0600)), "Symlink mode should match explicit FileMode")
	Expect(modes2["absolute-symlink.txt"]).To(Equal(int64(0600)), "Symlink mode should match explicit FileMode")
	
	// Directories should use FileMode when DirMode is not specified
	Expect(modes2["dir1"]).To(Equal(int64(0600)), "Directory mode should match explicit FileMode when DirMode is not set")

	// Test Case 3: Both FileMode and DirMode in attributes
	layer3, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/symlinks",
	}, schema.LayerAttributes{FileMode: 0600, DirMode: 0700})
	Expect(err).NotTo(HaveOccurred())

	modes3 := extractAndCheckModes(layer3, "FileMode and DirMode")
	
	// Verify FileMode is applied to files and symlinks
	Expect(modes3["testfile.txt"]).To(Equal(int64(0600)), "File mode should match explicit FileMode")
	Expect(modes3["dir1/dirfile.txt"]).To(Equal(int64(0600)), "File mode should match explicit FileMode")
	Expect(modes3["relative-symlink.txt"]).To(Equal(int64(0600)), "Symlink mode should match explicit FileMode")
	
	// Verify DirMode is applied to directories
	Expect(modes3["dir1"]).To(Equal(int64(0700)), "Directory mode should match explicit DirMode")

	// Test Case 4: Only DirMode in attributes
	layer4, err := localdir.FromFilesystem(localdir.From{
		Path: "./testdata/symlinks",
	}, schema.LayerAttributes{DirMode: 0700})
	Expect(err).NotTo(HaveOccurred())

	modes4 := extractAndCheckModes(layer4, "Only DirMode")
	
	// Verify default mode is applied to files and symlinks
	Expect(modes4["testfile.txt"]).To(Equal(int64(0644)), "File mode should be default when only DirMode is set")
	Expect(modes4["relative-symlink.txt"]).To(Equal(int64(0644)), "Symlink mode should be default when only DirMode is set")
	
	// Verify DirMode is applied to directories
	Expect(modes4["dir1"]).To(Equal(int64(0700)), "Directory mode should match explicit DirMode")
}

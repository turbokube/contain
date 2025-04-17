package localdir_test

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/localdir"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestModePreserve checks preservation of file and directory modes creatable by a regular user.
// Mode preserve might void reproducibility of builds,
// but until layer definitions have a fine grained way to specify at least executable bits
// we probably need it for structures like node_modules
func TestModePreserve(t *testing.T) {
	logger := zaptest.NewLogger(t)
	defer logger.Sync()
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	RegisterTestingT(t)

	tempDir, err := os.MkdirTemp("", "filemodeedge")
	Expect(err).NotTo(HaveOccurred())
	defer os.RemoveAll(tempDir)

	// Regular file with 0644 permissions
	file644 := filepath.Join(tempDir, "file644.txt")
	err = os.WriteFile(file644, []byte("readable"), 0644)
	Expect(err).NotTo(HaveOccurred())

	// Private file with 0600 permissions
	file600 := filepath.Join(tempDir, "file600.txt")
	err = os.WriteFile(file600, []byte("private"), 0600)
	Expect(err).NotTo(HaveOccurred())

	// Executable file with 0755 permissions
	exec755 := filepath.Join(tempDir, "exec755.sh")
	err = os.WriteFile(exec755, []byte("#!/bin/sh\necho hi"), 0755)
	Expect(err).NotTo(HaveOccurred())

	// Directory with 0755 permissions
	dir755 := filepath.Join(tempDir, "dir755")
	err = os.Mkdir(dir755, 0755)
	Expect(err).NotTo(HaveOccurred())

	// Directory with 0700 permissions
	dir700 := filepath.Join(tempDir, "dir700")
	err = os.Mkdir(dir700, 0700)
	Expect(err).NotTo(HaveOccurred())

	layer, err := localdir.FromFilesystem(localdir.From{
		Path: tempDir,
	}, schema.LayerAttributes{})
	Expect(err).NotTo(HaveOccurred())

	reader, err := layer.Uncompressed()
	Expect(err).NotTo(HaveOccurred())
	defer reader.Close()

	tr := tar.NewReader(reader)
	modes := make(map[string]int64)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		modes[h.Name] = h.Mode
	}

	t.Logf("Edge case modes: %+v", modes)
	Expect(modes["file644.txt"]&0777).To(Equal(int64(0644)), "file644.txt should have 0644 perms")
	Expect(modes["file600.txt"]&0777).To(Equal(int64(0600)), "file600.txt should have 0600 perms")
	Expect(modes["exec755.sh"]&0777).To(Equal(int64(0755)), "exec755.sh should have 0755 perms")
	Expect(modes["dir755"]&0777).To(Equal(int64(0755)), "dir755 should have 0755 perms")
	Expect(modes["dir700"]&0777).To(Equal(int64(0700)), "dir700 should have 0700 perms")
}

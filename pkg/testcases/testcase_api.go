package testcases

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/turbokube/contain/pkg/contain"
	schema "github.com/turbokube/contain/pkg/schema/v1"
)

type TestInput struct {
	// Registry is the current test registry
	Registry string
}

// TempDir offers actions on a temp directory.
type TempDir struct {
	t    *testing.T
	root string
}

type Testcase struct {
	// RunConfig produces a config and uses dir to populate a file system to run on
	RunConfig func(input *TestInput, dir *TempDir) schema.ContainConfig
	// ExpectDigest is mandatory, a sha256:-prefixed string
	// which if it matches the pushed digest passes the test without further expect calls
	ExpectDigest string
	// Expect will run on ExpectDigest mismatch
	Expect func(ref contain.Artifact, t *testing.T)
}

// NewTempDir creates a temporary directory and a teardown function
// that should be called to properly delete the directory content.
func NewTempDir(t *testing.T) *TempDir {
	root, err := os.MkdirTemp("", "skaffold")
	if err != nil {
		t.Error(err)
	}

	t.Cleanup(func() { os.RemoveAll(root) })

	return &TempDir{
		t:    t,
		root: root,
	}
}

// Root returns the temp directory.
func (h *TempDir) Root() string {
	return h.root
}

// Remove deletes a file from the temp directory.
func (h *TempDir) Remove(file string) *TempDir {
	return h.failIfErr(os.Remove(h.Path(file)))
}

// Chtimes changes the times for a file in the temp directory.
func (h *TempDir) Chtimes(file string, t time.Time) *TempDir {
	return h.failIfErr(os.Chtimes(h.Path(file), t, t))
}

// Mkdir makes a sub-directory in the temp directory.
func (h *TempDir) Mkdir(dir string) *TempDir {
	return h.failIfErr(os.MkdirAll(h.Path(dir), os.ModePerm))
}

// Write write content to a file in the temp directory.
func (h *TempDir) Write(file, content string) *TempDir {
	h.failIfErr(os.MkdirAll(filepath.Dir(h.Path(file)), os.ModePerm))
	return h.failIfErr(os.WriteFile(h.Path(file), []byte(content), os.ModePerm))
}

// WriteFiles write a list of files (path->content) in the temp directory.
func (h *TempDir) WriteFiles(files map[string]string) *TempDir {
	for path, content := range files {
		h.Write(path, content)
	}
	return h
}

// Touch creates a list of empty files in the temp directory.
func (h *TempDir) Touch(files ...string) *TempDir {
	for _, file := range files {
		h.Write(file, "")
	}
	return h
}

// Symlink creates a symlink.
func (h *TempDir) Symlink(dst, src string) *TempDir {
	h.failIfErr(os.MkdirAll(filepath.Dir(h.Path(src)), os.ModePerm))
	return h.failIfErr(os.Symlink(h.Path(dst), h.Path(src)))
}

// Rename renames a file from oldname to newname
func (h *TempDir) Rename(oldName, newName string) *TempDir {
	return h.failIfErr(os.Rename(h.Path(oldName), h.Path(newName)))
}

// Path returns the path to a file in the temp directory.
func (h *TempDir) Path(file string) string {
	elem := []string{h.root}
	elem = append(elem, strings.Split(file, "/")...)
	return filepath.Join(elem...)
}

func (h *TempDir) failIfErr(err error) *TempDir {
	if err != nil {
		h.t.Fatal(err)
	}
	return h
}

// Paths returns the paths to a list of files in the temp directory.
func (h *TempDir) Paths(files ...string) []string {
	var paths []string
	for _, file := range files {
		paths = append(paths, h.Path(file))
	}
	return paths
}

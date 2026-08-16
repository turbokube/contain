package ocipush

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStagingDir(t *testing.T) {
	t.Setenv(envStagingDir, "")
	t.Setenv("CONTAIN_CACHE_DIR", "")

	// nothing configured: os.CreateTemp's own default
	if got := resolveStagingDir(""); got != "" {
		t.Errorf("unconfigured staging dir = %q, want empty (system temp)", got)
	}

	// the cache dir a deployment already points at real disk carries staging
	t.Setenv("CONTAIN_CACHE_DIR", "/data/contain")
	if got, want := resolveStagingDir(""), filepath.Join("/data/contain", "staging"); got != want {
		t.Errorf("staging dir from cache dir = %q, want %q", got, want)
	}

	// the dedicated env var wins over the cache dir
	t.Setenv(envStagingDir, "/data/staging")
	if got := resolveStagingDir(""); got != "/data/staging" {
		t.Errorf("staging dir from env = %q, want /data/staging", got)
	}

	// an explicit option wins over both
	if got := resolveStagingDir("/explicit"); got != "/explicit" {
		t.Errorf("explicit staging dir = %q, want /explicit", got)
	}
}

// stagingFile must create the directory it was pointed at: an operator
// setting --staging-dir to a fresh volume mount should not have to mkdir it.
func TestStagingFileCreatesDir(t *testing.T) {
	t.Setenv(envStagingDir, "")
	t.Setenv("CONTAIN_CACHE_DIR", "")

	dir := filepath.Join(t.TempDir(), "nested", "staging")
	f, err := stagingFile(dir, "contain-test-*")
	if err != nil {
		t.Fatalf("stagingFile: %v", err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck
	defer f.Close()           //nolint:errcheck

	if !strings.HasPrefix(f.Name(), dir) {
		t.Errorf("staged into %q, want a file under %q", f.Name(), dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("staging dir not created: %v", err)
	}
}

// A staging dir that cannot be created has to say which one it was, since
// the whole point of the option is diagnosing where large blobs land.
func TestStagingFileErrorNamesTheDir(t *testing.T) {
	t.Setenv(envStagingDir, "")
	t.Setenv("CONTAIN_CACHE_DIR", "")

	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := stagingFile(filepath.Join(blocker, "under-a-file"), "contain-test-*")
	if err == nil {
		t.Fatal("expected an error staging under a regular file")
	}
	if !strings.Contains(err.Error(), blocker) {
		t.Errorf("error should name the staging dir, got: %v", err)
	}
}

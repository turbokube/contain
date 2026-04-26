package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveDir(t *testing.T) {
	t.Run("CONTAIN_CACHE_DIR takes precedence", func(t *testing.T) {
		t.Setenv("CONTAIN_CACHE_DIR", "/tmp/test-contain-cache")
		t.Setenv("XDG_CACHE_HOME", "/should/not/use")
		dir, err := resolveDir()
		if err != nil {
			t.Fatal(err)
		}
		if dir != "/tmp/test-contain-cache/layers" {
			t.Errorf("got %s", dir)
		}
	})

	t.Run("XDG_CACHE_HOME fallback", func(t *testing.T) {
		t.Setenv("CONTAIN_CACHE_DIR", "")
		t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
		dir, err := resolveDir()
		if err != nil {
			t.Fatal(err)
		}
		if dir != "/tmp/xdg-test/contain/layers" {
			t.Errorf("got %s", dir)
		}
	})

	t.Run("home fallback", func(t *testing.T) {
		t.Setenv("CONTAIN_CACHE_DIR", "")
		t.Setenv("XDG_CACHE_HOME", "")
		dir, err := resolveDir()
		if err != nil {
			t.Fatal(err)
		}
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".cache", "contain", "layers")
		if dir != expected {
			t.Errorf("got %s, want %s", dir, expected)
		}
	})
}

func TestEnabled(t *testing.T) {
	t.Run("default enabled", func(t *testing.T) {
		t.Setenv("CONTAIN_CACHE", "")
		if !Enabled() {
			t.Error("should be enabled by default")
		}
	})

	t.Run("disabled with false", func(t *testing.T) {
		t.Setenv("CONTAIN_CACHE", "false")
		if Enabled() {
			t.Error("should be disabled")
		}
	})

	t.Run("disabled with 0", func(t *testing.T) {
		t.Setenv("CONTAIN_CACHE", "0")
		if Enabled() {
			t.Error("should be disabled")
		}
	})
}

func TestNew(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONTAIN_CACHE_DIR", dir)

	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Dir() != filepath.Join(dir, "layers") {
		t.Errorf("got dir %s", c.Dir())
	}
	info, err := os.Stat(c.Dir())
	if err != nil {
		t.Fatal("cache dir not created")
	}
	if !info.IsDir() {
		t.Error("cache dir is not a directory")
	}
}

func TestPurgeAll(t *testing.T) {
	dir := t.TempDir()
	layersDir := filepath.Join(dir, "layers")
	os.MkdirAll(layersDir, 0700)

	// Create fake cache entries
	for _, name := range []string{"sha256:aaa", "sha256:bbb", "sha256:ccc"} {
		os.WriteFile(filepath.Join(layersDir, name), []byte("fake-layer"), 0644)
	}

	t.Setenv("CONTAIN_CACHE_DIR", dir)
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	count, bytes, err := c.Info()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 entries, got %d", count)
	}
	if bytes != 30 { // 3 * 10 bytes
		t.Errorf("expected 30 bytes, got %d", bytes)
	}

	result, err := c.Purge(PurgeStrategy{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedCount != 3 {
		t.Errorf("expected 3 removed, got %d", result.RemovedCount)
	}

	count, _, _ = c.Info()
	if count != 0 {
		t.Errorf("expected 0 after purge, got %d", count)
	}
}

func TestPurgeMaxAge(t *testing.T) {
	dir := t.TempDir()
	layersDir := filepath.Join(dir, "layers")
	os.MkdirAll(layersDir, 0700)

	old := filepath.Join(layersDir, "sha256:old")
	recent := filepath.Join(layersDir, "sha256:recent")
	os.WriteFile(old, []byte("old"), 0644)
	os.WriteFile(recent, []byte("recent"), 0644)

	// Set old file to 10 days ago
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(old, oldTime, oldTime)

	t.Setenv("CONTAIN_CACHE_DIR", dir)
	c, _ := New(nil)

	result, err := c.Purge(PurgeStrategy{MaxAge: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedCount != 1 {
		t.Errorf("expected 1 removed, got %d", result.RemovedCount)
	}
	if result.RetainedCount != 1 {
		t.Errorf("expected 1 retained, got %d", result.RetainedCount)
	}
}

func TestPurgeMaxSize(t *testing.T) {
	dir := t.TempDir()
	layersDir := filepath.Join(dir, "layers")
	os.MkdirAll(layersDir, 0700)

	// Create 3 entries of 100 bytes each, with different mtimes
	for i, name := range []string{"sha256:oldest", "sha256:middle", "sha256:newest"} {
		data := make([]byte, 100)
		os.WriteFile(filepath.Join(layersDir, name), data, 0644)
		mtime := time.Now().Add(time.Duration(i-3) * time.Hour)
		os.Chtimes(filepath.Join(layersDir, name), mtime, mtime)
	}

	t.Setenv("CONTAIN_CACHE_DIR", dir)
	c, _ := New(nil)

	// 300 bytes total, keep max 150 — must evict 2 oldest, keep 1 newest
	result, err := c.Purge(PurgeStrategy{MaxSize: 150})
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedCount != 2 {
		t.Errorf("expected 2 removed, got %d", result.RemovedCount)
	}
	if result.RetainedCount != 1 {
		t.Errorf("expected 1 retained, got %d", result.RetainedCount)
	}
}

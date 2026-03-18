package pushlock

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewFlockPushLock_RejectsRelativePath(t *testing.T) {
	_, err := NewFlockPushLock("relative/path.lock", zap.NewNop())
	if err == nil {
		t.Fatal("expected error for relative path")
	}
}

func TestNewFlockPushLock_RejectsEmptyPath(t *testing.T) {
	_, err := NewFlockPushLock("", zap.NewNop())
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFlockPushLock_AcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	logger := zaptest.NewLogger(t)

	lock, err := NewFlockPushLock(path, logger)
	if err != nil {
		t.Fatal(err)
	}

	release, err := lock.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Lock file should exist with metadata
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("lock file should contain holder metadata")
	}

	release()
}

func TestFlockPushLock_Serializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	logger := zaptest.NewLogger(t)

	lock, err := NewFlockPushLock(path, logger)
	if err != nil {
		t.Fatal(err)
	}

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := lock.Acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			defer release()

			n := concurrent.Add(1)
			if n > maxConcurrent.Load() {
				maxConcurrent.Store(n)
			}
			// Simulate push work
			time.Sleep(10 * time.Millisecond)
			concurrent.Add(-1)
		}()
	}

	wg.Wait()

	if maxConcurrent.Load() > 1 {
		t.Errorf("expected max concurrency 1, got %d", maxConcurrent.Load())
	}
}

func TestFlockPushLock_ContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	logger := zaptest.NewLogger(t)

	lock, err := NewFlockPushLock(path, logger)
	if err != nil {
		t.Fatal(err)
	}

	// Hold the lock
	release, err := lock.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Try to acquire with a cancelled context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = lock.Acquire(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}

	release()
}

func TestFlockPushLock_HolderMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	logger := zaptest.NewLogger(t)

	lock, err := NewFlockPushLock(path, logger)
	if err != nil {
		t.Fatal(err)
	}

	release, err := lock.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !contains(content, "pid=") {
		t.Errorf("expected pid in metadata, got %q", content)
	}
	if !contains(content, "started=") {
		t.Errorf("expected started in metadata, got %q", content)
	}

	release()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

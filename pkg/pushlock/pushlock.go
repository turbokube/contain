// Package pushlock serializes registry push operations across OS processes.
package pushlock

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// PushLock serializes registry push operations across processes.
// Implementations must be safe for use across OS processes on the same machine.
type PushLock interface {
	// Acquire blocks until this process may push.
	// The returned release function MUST be called when the push completes or fails.
	// Context cancellation unblocks a waiting Acquire.
	Acquire(ctx context.Context) (release func(), err error)
}

// FlockPushLock uses flock(2) advisory locking on a shared file.
// The lock is automatically released by the kernel if the process dies.
// The lock file contains metadata about the current holder for diagnostics.
type FlockPushLock struct {
	path   string
	logger *zap.Logger
}

// NewFlockPushLock creates a PushLock backed by flock on the given path.
// The path must be absolute.
func NewFlockPushLock(path string, logger *zap.Logger) (*FlockPushLock, error) {
	if path == "" {
		return nil, fmt.Errorf("push lock path must not be empty")
	}
	if path[0] != '/' {
		return nil, fmt.Errorf("push lock path must be absolute, got %q", path)
	}
	return &FlockPushLock{path: path, logger: logger}, nil
}

// Acquire blocks until the flock is obtained. While waiting, it reads
// the lock file to log who currently holds the lock.
func (l *FlockPushLock) Acquire(ctx context.Context) (func(), error) {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("push lock open %s: %w", l.path, err)
	}

	start := time.Now()

	// Try non-blocking first
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Lock is held by another process — log who
		holder := readHolder(f)
		l.logger.Info("push lock waiting",
			zap.String("path", l.path),
			zap.String("holder", holder),
		)

		// Block until acquired, checking context in a goroutine
		acquired := make(chan error, 1)
		go func() {
			acquired <- syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		}()

		select {
		case err = <-acquired:
			if err != nil {
				f.Close()
				return nil, fmt.Errorf("push lock flock %s: %w", l.path, err)
			}
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		}

		l.logger.Info("push lock acquired",
			zap.String("path", l.path),
			zap.Duration("waited", time.Since(start)),
		)
	}

	// Write metadata about current holder
	writeHolder(f)

	release := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	return release, nil
}

func writeHolder(f *os.File) {
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "pid=%d\nstarted=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
}

func readHolder(f *os.File) string {
	f.Seek(0, 0)
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	if n == 0 {
		return "(unknown)"
	}
	return string(buf[:n])
}

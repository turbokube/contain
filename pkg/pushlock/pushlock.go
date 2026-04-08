// Package pushlock serializes registry push operations across OS processes.
package pushlock

import (
	"context"
	"fmt"
	"os"
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
	if err := validateAbsPath(path); err != nil {
		return nil, err
	}
	return &FlockPushLock{path: path, logger: logger}, nil
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

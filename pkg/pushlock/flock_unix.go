//go:build !windows

package pushlock

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func validateAbsPath(path string) error {
	if path[0] != '/' {
		return fmt.Errorf("push lock path must be absolute, got %q", path)
	}
	return nil
}

// Acquire blocks until the flock is obtained. While waiting, it reads
// the lock file to log who currently holds the lock.
func (l *FlockPushLock) Acquire(ctx context.Context) (func(), error) {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("push lock open %s: %w", l.path, err)
	}

	// Poll with LOCK_NB so context cancellation works reliably across platforms
	var waited bool
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}

		if !waited {
			// First failed attempt — log who holds the lock
			holder := readHolder(f)
			l.logger.Info("push lock waiting",
				zap.String("path", l.path),
				zap.String("holder", holder),
			)
			waited = true
		}

		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Write metadata about current holder
	writeHolder(f)

	release := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	return release, nil
}

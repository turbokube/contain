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

	start := time.Now()

	// Try non-blocking first
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Lock is held by another process, log who
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

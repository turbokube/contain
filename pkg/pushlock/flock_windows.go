//go:build windows

package pushlock

import (
	"context"
	"fmt"
	"path/filepath"
)

func validateAbsPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("push lock path must be absolute, got %q", path)
	}
	return nil
}

// Acquire on Windows returns an error because flock is not available.
func (l *FlockPushLock) Acquire(ctx context.Context) (func(), error) {
	return nil, fmt.Errorf("push lock is not supported on Windows")
}

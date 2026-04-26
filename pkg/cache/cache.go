// Package cache provides a per-user base image layer cache backed by
// go-containerregistry's filesystem cache. Layers are stored by digest
// in a flat directory, giving automatic deduplication across base images.
package cache

import (
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	v1cache "github.com/google/go-containerregistry/pkg/v1/cache"
	"go.uber.org/zap"
)

const (
	envCacheDir     = "CONTAIN_CACHE_DIR"
	envCacheEnabled = "CONTAIN_CACHE"
	subdir          = "layers"
)

// BaseImageCache wraps go-containerregistry's filesystem cache with
// access-time tracking for LRU purging and concurrent-write safety.
type BaseImageCache struct {
	inner  v1cache.Cache
	dir    string
	logger *zap.Logger
}

// Enabled returns false only if CONTAIN_CACHE is explicitly "false" or "0".
func Enabled() bool {
	v := os.Getenv(envCacheEnabled)
	return v != "false" && v != "0"
}

// New creates a cache at the resolved directory. The directory is created
// if it does not exist.
func New(logger *zap.Logger) (*BaseImageCache, error) {
	dir, err := resolveDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create cache dir %s: %w", dir, err)
	}
	inner := &safeCache{
		dir:   dir,
		inner: v1cache.NewFilesystemCache(dir),
	}
	return &BaseImageCache{
		inner:  inner,
		dir:    dir,
		logger: logger,
	}, nil
}

// WrapImage wraps a v1.Image so its layers are read from cache on hit,
// and written to cache as they are consumed on miss.
func (c *BaseImageCache) WrapImage(img v1.Image) v1.Image {
	return v1cache.Image(img, c.inner)
}

// Dir returns the cache directory path.
func (c *BaseImageCache) Dir() string {
	return c.dir
}

// LogSummary logs cache hit/miss stats at info level.
// Call once after a build completes.
func (c *BaseImageCache) LogSummary() {
	sc := c.inner.(*safeCache)
	hits := sc.hits.Load()
	puts := sc.puts.Load()
	if hits+puts == 0 {
		return
	}
	c.logger.Info("layer cache",
		zap.Int64("hits", hits),
		zap.Int64("stored", puts),
		zap.String("dir", c.dir),
	)
}

func resolveDir() (string, error) {
	if d := os.Getenv(envCacheDir); d != "" {
		return filepath.Join(d, subdir), nil
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "contain", subdir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".cache", "contain", subdir), nil
}

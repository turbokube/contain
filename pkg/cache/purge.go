package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PurgeStrategy configures which cache entries to remove.
type PurgeStrategy struct {
	MaxAge  time.Duration // remove entries older than this (by mtime)
	MaxSize int64         // remove oldest entries until total size is at or below this
	All     bool          // remove everything
}

// PurgeResult reports what happened.
type PurgeResult struct {
	RemovedCount  int
	RemovedBytes  int64
	RetainedCount int
	RetainedBytes int64
}

type cacheEntry struct {
	path  string
	size  int64
	mtime time.Time
}

// Purge removes cache entries according to the given strategy.
func (c *BaseImageCache) Purge(strategy PurgeStrategy) (PurgeResult, error) {
	entries, err := listEntries(c.dir)
	if err != nil {
		return PurgeResult{}, err
	}

	var result PurgeResult
	toRemove := make(map[string]bool)

	if strategy.All {
		for _, e := range entries {
			toRemove[e.path] = true
		}
	} else {
		if strategy.MaxAge > 0 {
			cutoff := time.Now().Add(-strategy.MaxAge)
			for _, e := range entries {
				if e.mtime.Before(cutoff) {
					toRemove[e.path] = true
				}
			}
		}
		if strategy.MaxSize > 0 {
			// Sort oldest first for LRU eviction
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].mtime.Before(entries[j].mtime)
			})
			var totalSize int64
			for _, e := range entries {
				totalSize += e.size
			}
			for _, e := range entries {
				if totalSize <= strategy.MaxSize {
					break
				}
				toRemove[e.path] = true
				totalSize -= e.size
			}
		}
	}

	// Also remove orphaned .tmp files from interrupted writes
	tmps, _ := filepath.Glob(filepath.Join(c.dir, "*.tmp.*"))
	for _, t := range tmps {
		toRemove[t] = true
	}

	for _, e := range entries {
		if toRemove[e.path] {
			if err := os.Remove(e.path); err != nil && !os.IsNotExist(err) {
				return result, fmt.Errorf("remove %s: %w", e.path, err)
			}
			result.RemovedCount++
			result.RemovedBytes += e.size
		} else {
			result.RetainedCount++
			result.RetainedBytes += e.size
		}
	}

	return result, nil
}

// Info returns the current cache entry count and total size.
func (c *BaseImageCache) Info() (count int, totalBytes int64, err error) {
	entries, err := listEntries(c.dir)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		totalBytes += e.size
	}
	return len(entries), totalBytes, nil
}

func listEntries(dir string) ([]cacheEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []cacheEntry
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		// Only cache blobs (sha256:... or sha256-... on windows)
		if !strings.HasPrefix(de.Name(), "sha256") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		entries = append(entries, cacheEntry{
			path:  filepath.Join(dir, de.Name()),
			size:  info.Size(),
			mtime: info.ModTime(),
		})
	}
	return entries, nil
}

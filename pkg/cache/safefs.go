package cache

import (
	"os"
	"sync/atomic"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	v1cache "github.com/google/go-containerregistry/pkg/v1/cache"
)

// safeCache wraps a v1cache.Cache adding:
// - stat-check before Put to skip existing blobs (concurrent build safety)
// - mtime update on Get for LRU tracking
// - hit/miss counters for summary logging
type safeCache struct {
	dir   string
	inner v1cache.Cache
	hits  atomic.Int64
	puts  atomic.Int64
}

func (s *safeCache) Put(l v1.Layer) (v1.Layer, error) {
	digest, err := l.Digest()
	if err != nil {
		return s.inner.Put(l)
	}
	path := cachepath(s.dir, digest)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		if cached, err := s.inner.Get(digest); err == nil {
			s.hits.Add(1)
			return cached, nil
		}
	}
	s.puts.Add(1)
	return s.inner.Put(l)
}

func (s *safeCache) Get(h v1.Hash) (v1.Layer, error) {
	l, err := s.inner.Get(h)
	if err != nil {
		return l, err
	}
	s.hits.Add(1)
	path := cachepath(s.dir, h)
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return l, nil
}

func (s *safeCache) Delete(h v1.Hash) error {
	return s.inner.Delete(h)
}

func cachepath(dir string, h v1.Hash) string {
	return dir + "/" + h.String()
}

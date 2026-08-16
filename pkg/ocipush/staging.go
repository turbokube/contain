package ocipush

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// envStagingDir overrides where blobs are staged, for callers that cannot set
// StagingDir programmatically.
const envStagingDir = "CONTAIN_STAGING_DIR"

// stagingFile creates the temp file a blob is staged in before it is
// re-uploaded. Both mirror and the registry-proxy need the blob on disk
// rather than streamed through: the digest has to be verified before
// anything is pushed, and the direct-upload path reads parts by offset.
//
// Location matters more than it looks. The default, os.TempDir(), is /tmp,
// which in a container or a pod with a default emptyDir is frequently tmpfs —
// that is, RAM. The registry-proxy's documented deployment is exactly that,
// and it stages whole layers, so a multi-GB push would be charged to memory
// and OOM-killed with no way to redirect it. Resolution order:
//
//  1. dir, when the caller set Options.StagingDir
//  2. $CONTAIN_STAGING_DIR
//  3. $CONTAIN_CACHE_DIR/staging, so a deployment that already points
//     contain's cache at real disk gets staging there too
//  4. os.TempDir()
func stagingFile(dir string, pattern string) (*os.File, error) {
	resolved := resolveStagingDir(dir)
	if resolved != "" {
		if err := os.MkdirAll(resolved, 0700); err != nil {
			return nil, fmt.Errorf("create staging dir %s: %w", resolved, err)
		}
	}
	f, err := os.CreateTemp(resolved, pattern)
	if err != nil {
		return nil, fmt.Errorf("create staging file in %s: %w", stagingDirName(resolved), err)
	}
	return f, nil
}

func resolveStagingDir(dir string) string {
	if dir != "" {
		return dir
	}
	if d := os.Getenv(envStagingDir); d != "" {
		return d
	}
	if d := os.Getenv("CONTAIN_CACHE_DIR"); d != "" {
		return filepath.Join(d, "staging")
	}
	// empty means os.CreateTemp's default, os.TempDir()
	return ""
}

func stagingDirName(resolved string) string {
	if resolved == "" {
		return os.TempDir()
	}
	return resolved
}

// logStagingDir reports once where blobs will be staged, so an operator
// hitting a full or memory-backed filesystem can see which one it was.
func logStagingDir(dir string) {
	zap.L().Debug("staging blobs", zap.String("dir", stagingDirName(resolveStagingDir(dir))))
}

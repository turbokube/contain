package v1

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// ResolveLocalFilePath returns the source path to use for this platform.
// An empty return value means neither PathPerPlatform nor Path is set for
// this platform; callers must treat that as an error.
//
// Matching order:
//  1. PathPerPlatform[<os>/<arch>/<variant>] (exact, when variant is present)
//  2. PathPerPlatform[<os>/<arch>] (drop variant/os.version)
//  3. Path (fallback)
func ResolveLocalFilePath(lf LocalFile, platform v1.Platform) string {
	if len(lf.PathPerPlatform) > 0 {
		if platform.Variant != "" {
			if p := lf.PathPerPlatform[platform.OS+"/"+platform.Architecture+"/"+platform.Variant]; p != "" {
				return p
			}
		}
		if p := lf.PathPerPlatform[platform.OS+"/"+platform.Architecture]; p != "" {
			return p
		}
	}
	return lf.Path
}

// ValidateLayers checks that every layer has a resolvable source for every
// platform in platforms. Returns a nil error if everything resolves, or an
// error naming each offending layer and platform. The error is suitable
// for early-exit before any registry push.
func ValidateLayers(config ContainConfig, platforms []v1.Platform) error {
	var errs []string
	for i, layer := range config.Layers {
		hasLocalFile := layer.LocalFile.Path != "" || len(layer.LocalFile.PathPerPlatform) > 0
		hasLocalDir := layer.LocalDir.Path != ""
		if hasLocalFile && hasLocalDir {
			errs = append(errs, fmt.Sprintf("layers[%d]: each layer item must have exactly one type, got localFile and localDir", i))
			continue
		}
		if !hasLocalFile && !hasLocalDir {
			errs = append(errs, fmt.Sprintf("layers[%d]: no layer builder config found (set localFile.path, localFile.pathPerPlatform, or localDir.path)", i))
			continue
		}
		if !hasLocalFile {
			continue
		}
		keys := make([]string, 0, len(layer.LocalFile.PathPerPlatform))
		for k := range layer.LocalFile.PathPerPlatform {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !isValidPlatformKey(key) {
				errs = append(errs, fmt.Sprintf(`layers[%d].localFile.pathPerPlatform: invalid key %q (expected "<os>/<arch>" or "<os>/<arch>/<variant>")`, i, key))
			}
		}
		for _, p := range platforms {
			if ResolveLocalFilePath(layer.LocalFile, p) == "" {
				errs = append(errs, fmt.Sprintf(`layers[%d].localFile: no path for platform %s (add pathPerPlatform[%q] or a top-level path fallback)`, i, p.String(), p.OS+"/"+p.Architecture))
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// isValidPlatformKey accepts "<os>/<arch>" and "<os>/<arch>/<variant>"
// with non-empty segments. Looser forms (single segment, trailing slash,
// whitespace) are rejected.
func isValidPlatformKey(key string) bool {
	if strings.ContainsAny(key, " \t\n") {
		return false
	}
	parts := strings.Split(key, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

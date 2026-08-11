package v1

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/turbokube/contain/pkg/platform"
)

// ResolveLocalFilePath returns the source path to use for this platform.
// An empty return value means neither PathPerPlatform nor Path is set for
// this platform; callers must treat that as an error.
//
// Matching order:
//  1. PathPerPlatform[<os>/<arch>/<variant>] (exact, when variant is present)
//  2. a key that denotes the same platform once normalized, so a key written
//     linux/arm64 serves a base child declaring linux/arm64/v8 and the other
//     way round. This is the same comparison base index children get, so the
//     two cannot disagree about which key serves which platform.
//  3. PathPerPlatform[<os>/<arch>] (drop variant/os.version)
//  4. Path (fallback)
//
// Step 2 is what handles the direction dropping a variant cannot: a key
// linux/arm64/v8 serving a base child that declares plain linux/arm64.
//
// Step 3 predates normalization and is kept, so resolution is a superset of
// what it was: a key naming no variant is a deliberate "this file serves the
// architecture", and still covers variants normalization treats as distinct,
// linux/amd64/v3 and linux/arm/v6 among them. That coarseness is safe here,
// where the answer is which file to read, and would not be for base
// selection, where matching two children of one requested platform would
// publish an image nobody asked for.
func ResolveLocalFilePath(lf LocalFile, p v1.Platform) string {
	if len(lf.PathPerPlatform) > 0 {
		if p.Variant != "" {
			if path := lf.PathPerPlatform[p.OS+"/"+p.Architecture+"/"+p.Variant]; path != "" {
				return path
			}
		}
		// sorted so that two keys normalizing to the same platform resolve
		// the same way on every run
		keys := make([]string, 0, len(lf.PathPerPlatform))
		for k := range lf.PathPerPlatform {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			kp, err := v1.ParsePlatform(k)
			if err != nil {
				continue
			}
			if platform.Equal(*kp, p) && lf.PathPerPlatform[k] != "" {
				return lf.PathPerPlatform[k]
			}
		}
		if path := lf.PathPerPlatform[p.OS+"/"+p.Architecture]; path != "" {
			return path
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

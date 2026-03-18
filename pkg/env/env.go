package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// PushOption returns the value of CONTAIN_PUSH as a *bool.
// Returns nil if the env is not set.
func PushOption() (*bool, error) {
	v, ok := os.LookupEnv("CONTAIN_PUSH")
	if !ok {
		return nil, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return nil, fmt.Errorf("invalid CONTAIN_PUSH value %q: %w", v, err)
	}
	return &b, nil
}

// OCIOutputOption holds the result of reading CONTAIN_OCI_OUTPUT.
type OCIOutputOption struct {
	Path string
}

// OCIOutput returns non-nil if CONTAIN_OCI_OUTPUT is set.
// Returns error if the path is absolute (only relative paths are supported via env).
func OCIOutput() (*OCIOutputOption, error) {
	v, ok := os.LookupEnv("CONTAIN_OCI_OUTPUT")
	if !ok || v == "" {
		return nil, nil
	}
	if filepath.IsAbs(v) {
		return nil, fmt.Errorf("CONTAIN_OCI_OUTPUT must be a relative path, got %q", v)
	}
	return &OCIOutputOption{Path: v}, nil
}

// TurboHash returns the value of TURBO_HASH if set, empty string otherwise.
func TurboHash() string {
	return os.Getenv("TURBO_HASH")
}

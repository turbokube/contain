package v1

import "time"

type ContainConfig struct {
	Status ContainConfigStatus `json:"-"`
	// Base is the base image reference
	Base string `json:"base,omitempty" skaffold:"template"`
	// Tag is the result reference to be pushed
	Tag       string            `json:"tag,omitempty" skaffold:"template"`
	Platforms []string          `json:"platforms,omitempty"`
	Layers    []Layer           `json:"layers,omitempty"`
	Sync      ContainConfigSync `json:"-"`
}

type ContainConfigStatus struct {
	Template  bool   // true if config is from a template
	Md5       string // config source md5 (not for template)
	Sha256    string // config source sha256 (not for template)
	Overrides ContainConfigOverrides
}

type ContainConfigOverrides struct {
	Base bool
}

type ContainConfigSync struct {
	PodSelector     string
	Namespace       string
	GetAttemptsMax  int
	GetAttemptsWait time.Duration
}

type Layer struct {
	Attributes LayerAttributes `json:"layerAttributes,omitempty"`
	// exactly one of the following
	LocalDir  LocalDir  `json:"localDir,omitempty"`
	LocalFile LocalFile `json:"localFile,omitempty"`
}

type LayerAttributes struct {
	// generic, supported for applicable layer types
	Uid uint16 `json:"uid,omitempty"`
	Gid uint16 `json:"gid,omitempty"`

	// Mode bits to use on files, must be a value between 0 and 0777.
	// YAML accepts both octal and decimal values, JSON requires decimal values for mode bits.
	// Note that if you don't specify mode, contain will try to preserve local mode which might void reproducibility.
	FileMode int32 `json:"mode,omitempty"`

	// DirMode bits to use on directories, must be a value between 0 and 0777.
	// If not specified, the mode value will be used for directories as well.
	// YAML accepts both octal and decimal values, JSON requires decimal values for mode bits.
	// Note that if you don't specify mode, contain will try to preserve local mode which might void reproducibility.
	DirMode int32 `json:"dirMode,omitempty"`
}

// LocalFile is a single file that should be appended as-is to base
// with an optional path prefix, for example ./target/runner to /runner
type LocalFile struct {
	Path          string `json:"path" skaffold:"filepath,template"`
	ContainerPath string `json:"containerPath,omitempty" skaffold:"template"`
	MaxSize       string `json:"maxSize,omitempty" skaffold:"template"`
}

// LocalDir is a directory structure that should be appended as-is to base
// with an optional path prefix, for example ./target/app to /app
type LocalDir struct {
	Path          string   `json:"path" skaffold:"filepath,template"`
	ContainerPath string   `json:"containerPath,omitempty" skaffold:"template"`
	Ignore        []string `json:"ignore,omitempty" skaffold:"template"`
	MaxFiles      int      `json:"maxFiles,omitempty"`
	MaxSize       string   `json:"maxSize,omitempty" skaffold:"template"`
}

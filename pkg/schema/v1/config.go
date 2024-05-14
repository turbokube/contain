package v1

import "time"

type ContainConfig struct {
	Status ContainConfigStatus
	// Base is the base image reference
	Base string `yaml:"base" skaffold:"template"`
	// Tag is the result reference to be pushed
	Tag       string   `yaml:"tag" skaffold:"template"`
	Platforms []string `yaml:"platforms"`
	Layers    []Layer  `yaml:"layers,omitempty"`
	Sync      ContainConfigSync
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
	Attributes LayerAttributes `yaml:"layerAttributes,omitempty"`
	// exactly one of the following
	LocalDir LocalDir `yaml:"localDir,omitempty" skaffold:"template"`
}

type LayerAttributes struct {
	// generic, supported for applicable layer types
	Uid uint16 `yaml:"uid,omitempty" skaffold:"template"`
	Gid uint16 `yaml:"gid,omitempty" skaffold:"template"`

	// Mode bits to use on files, must be a value between 0 and 0777.
	// YAML accepts both octal and decimal values, JSON requires decimal values for mode bits.
	FileMode int32 `yaml:"fileMode,omitempty" skaffold:"template"`
}

// LocalDir is a directory structure that should be appended as-is to base
// with an optional path prefix, for example ./target/app to /app
type LocalDir struct {
	Path          string   `yaml:"path" skaffold:"filepath,template"`
	ContainerPath string   `yaml:"containerPath,omitempty" skaffold:"template"`
	Ignore        []string `yaml:"ignore,omitempty" skaffold:"template"`
	MaxFiles      int      `yaml:"maxFiles,omitempty" skaffold:"template"`
	MaxSize       string   `yaml:"maxSize,omitempty" skaffold:"template"`
}

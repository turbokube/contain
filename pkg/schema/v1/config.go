package v1

type ContainConfig struct {
	// Base is the base image reference
	Base string `yaml:"base"`
	// Tag is the result reference to be pushed
	Tag       string   `yaml:"tag"`
	Platforms []string `yaml:"platforms"`
	Layers    []Layer  `yaml:"layers,omitempty"`
}

type Layer struct {
	// exactly one of the following
	LocalDir LocalDir `yaml:"localDir,omitempty"`
}

// LocalDir is a directory structure that should be appended as-is to base
// with an optional path prefix, for example ./target/app to /app
type LocalDir struct {
	Path          string   `yaml:"path"`
	ContainerPath string   `yaml:"containerPath,omitempty"`
	Ignore        []string `yaml:"ignore,omitempty"`
}

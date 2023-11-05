module github.com/turbokube/contain

go 1.20

require (
	github.com/google/go-containerregistry v0.16.1
	github.com/moby/patternmatcher v0.5.0
	github.com/opencontainers/image-spec v1.1.0-rc3
	github.com/spf13/afero v1.9.5
	go.uber.org/zap v1.24.0
	gopkg.in/yaml.v3 v3.0.1
)

// test dependencies
require (
	github.com/distribution/distribution/v3 v3.0.0-20231017204442-915ad2d5a607
	github.com/phayes/freeport v0.0.0-20220201140144-74d24b5ae9f5
	github.com/sirupsen/logrus v1.9.1
)

require (
	github.com/benbjohnson/clock v1.1.0 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.14.3 // indirect
	github.com/docker/cli v24.0.0+incompatible // indirect
	github.com/docker/distribution v2.8.2+incompatible // indirect
	github.com/docker/docker v24.0.0+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.7.0 // indirect
	github.com/klauspost/compress v1.16.5 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sirupsen/logrus v1.9.1 // indirect
	github.com/vbatts/tar-split v0.11.3 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/sync v0.2.0 // indirect
	golang.org/x/sys v0.8.0 // indirect
	golang.org/x/text v0.8.0 // indirect
)

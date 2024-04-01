package registry

import (
	"regexp"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"go.uber.org/zap"
)

var (
	insecurAccessRefs = regexp.MustCompile(`^[^/]+\.local/`)
)

type RegistryConfig struct {
	CraneOptions crane.Options
}

func New(config schema.ContainConfig) (*RegistryConfig, error) {
	c := &RegistryConfig{}
	// https://github.com/google/go-containerregistry/blob/v0.13.0/pkg/crane/options.go#L43
	c.CraneOptions = crane.Options{
		Remote: []remote.Option{
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
		},
		Keychain: authn.DefaultKeychain,
	}

	if insecurAccessRefs.Match([]byte(config.Base)) {
		zap.L().Debug("insecure access enabled", zap.String("base", config.Base))
		crane.Insecure(&c.CraneOptions)
	}

	return c, nil
}

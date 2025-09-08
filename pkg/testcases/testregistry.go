package testcases

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	_ "github.com/distribution/distribution/v3/registry/auth/htpasswd"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/phayes/freeport"
	registryconfig "github.com/turbokube/contain/pkg/registry"
)

const (
	baseRegistry      = "../../test/baseregistry"
	baseImageNoattest = "test/baseimages/multiarch-test-noattest.oci/"
)

type TestRegistry struct {
	ctx context.Context
	// rootdirectory is where registry data is stored when not using inmemory
	rootdirectory string
	// Host is the start of image URLs up to but excluding the first slash (after .Start)
	Host string
	// CraneOptions configures go-containerregistry for access to this registry (after .Start)
	Config registryconfig.RegistryConfig
}

func NewTestregistry(ctx context.Context) *TestRegistry {
	root, err := filepath.Abs(baseRegistry)
	// abs with two levels up is unlikely to fail from a package,
	// and things that might actually fail should be in .Start
	if err != nil {
		panic(fmt.Errorf("abs %v", err))
	}

	return &TestRegistry{
		ctx:           ctx,
		rootdirectory: root,
	}
}

func (r *TestRegistry) Start() error {
	config := &configuration.Configuration{}
	config.Log.AccessLog.Disabled = true
	config.Log.Level = "error"
	port, err := freeport.GetFreePort()
	if err != nil {
		return fmt.Errorf("failed to get free port: %s", err)
	}

	r.Host = fmt.Sprintf("localhost:%d", port)
	config.HTTP.Addr = fmt.Sprintf("127.0.0.1:%d", port)
	config.HTTP.DrainTimeout = time.Duration(10) * time.Second
	// fast ephemeral
	config.Storage = map[string]configuration.Parameters{"inmemory": map[string]interface{}{}}
	// can be kept for debugging, can be pre-populated
	if r.rootdirectory == "" {
		fmt.Println("    test registry is ephemeral")
	} else {
		fmt.Printf("    test registry root: %s\n", r.rootdirectory)
		config.Storage = map[string]configuration.Parameters{
			"filesystem": map[string]interface{}{
				"rootdirectory": r.rootdirectory,
			},
			"delete": map[string]interface{}{
				"enabled": true,
			},
		}
	}

	dockerRegistry, err := registry.NewRegistry(r.ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create docker registry: %w", err)
	}

	go dockerRegistry.ListenAndServe()

	r.Config = registryconfig.RegistryConfig{
		CraneOptions: crane.Options{
			// Force anonymous auth for local test registry to avoid spawning
			// docker-credential-helpers via authn.DefaultKeychain.
			Remote: []remote.Option{remote.WithAuth(authn.Anonymous)},
		},
	}

	return nil
}

// loadBaseImages reads from image exports, see caveats with loadBaseImage
func (r *TestRegistry) LoadBaseImages() error {
	return r.loadBaseImage(
		baseImageNoattest,
		fmt.Sprintf("%s/solsson/multiarch-test:noattest", r.Host),
		"sha256:c6dde17b43016c18361cf6b2db724b84312f074f9cb332438bc3908ac603f995",
	)
}

// loadBaseImage is unused because it did not preserve digests
// neither using OCI nor tar format
// Instead we use ./testregistry-setup.sh and start testregistry from the resulting root dir
func (r *TestRegistry) loadBaseImage(path string, image string, digest string) error {
	abspath, err := filepath.Abs("../../" + path)
	if err != nil {
		return fmt.Errorf("absolute path for %s: %w", path, err)
	}
	img, err := layout.ImageIndexFromPath(abspath)
	if err != nil {
		return fmt.Errorf("loading %s as OCI layout: %w", path, err)
	}
	ref, err := name.ParseReference(image, r.Config.CraneOptions.Name...)
	if err != nil {
		return err
	}
	var h v1.Hash
	switch t := img.(type) {
	case v1.ImageIndex:
		if err := remote.WriteIndex(ref, t, r.Config.CraneOptions.Remote...); err != nil {
			return err
		}
		if h, err = t.Digest(); err != nil {
			return err
		}
		if h.String() != digest {
			return fmt.Errorf("wrote digest %s but epected %s", h, digest)
		}
	default:
		return fmt.Errorf("cannot push type (%T) to registry", img)
	}
	return nil
}

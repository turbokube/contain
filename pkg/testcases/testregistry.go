package testcases

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	"github.com/distribution/distribution/v3/registry"
	_ "github.com/distribution/distribution/v3/registry/auth/htpasswd"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/phayes/freeport"
	"github.com/sirupsen/logrus"
	registryconfig "github.com/turbokube/contain/pkg/registry"
)

const (
	baseImageNoattest  = "test/baseimages/multiarch-test-noattest.oci/"
	testRunDurationEnv = "TEST_REGISTRY_RUN"
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
	root, err := filepath.Abs("../../test/baseregistry")
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
	logger := newTestRegistryLogger()
	dcontext.SetDefaultLogger(logger)
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
		CraneOptions: crane.Options{},
	}

	return nil
}

// loadBaseImages reads from image exports, see caveats with loadBaseImage
func (r *TestRegistry) LoadBaseImages() error {
	return r.loadBaseImage(
		baseImageNoattest,
		fmt.Sprintf("%s/contain-test/multiarch-base", r.Host),
		"sha256:5df9572dfc5f15f997d84d002274cda07ba5e10d80b667fdd788f9abb9ebf15a",
	)
}

// loadBaseImage is unused because it did not preserve digests
// for a multi-arch source image (solsson/multiarch-test)
// neither using OCI nor tar format
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

type testRegistryLogger struct {
}

func newTestRegistryLogger() *testRegistryLogger {
	return &testRegistryLogger{}
}

// https://github.com/distribution/distribution/blob/v2.8.3/context/logger.go#L12
func (l *testRegistryLogger) Print(args ...interface{})                 {}
func (l *testRegistryLogger) Printf(format string, args ...interface{}) {}
func (l *testRegistryLogger) Println(args ...interface{})               {}
func (l *testRegistryLogger) Fatal(args ...interface{})                 {}
func (l *testRegistryLogger) Fatalf(format string, args ...interface{}) {}
func (l *testRegistryLogger) Fatalln(args ...interface{})               {}
func (l *testRegistryLogger) Panic(args ...interface{})                 {}
func (l *testRegistryLogger) Panicf(format string, args ...interface{}) {}
func (l *testRegistryLogger) Panicln(args ...interface{})               {}
func (l *testRegistryLogger) Debug(args ...interface{})                 {}
func (l *testRegistryLogger) Debugf(format string, args ...interface{}) {}
func (l *testRegistryLogger) Debugln(args ...interface{})               {}
func (l *testRegistryLogger) Error(args ...interface{})                 {}
func (l *testRegistryLogger) Errorf(format string, args ...interface{}) {}
func (l *testRegistryLogger) Errorln(args ...interface{})               {}
func (l *testRegistryLogger) Info(args ...interface{})                  {}
func (l *testRegistryLogger) Infof(format string, args ...interface{})  {}
func (l *testRegistryLogger) Infoln(args ...interface{})                {}
func (l *testRegistryLogger) Warn(args ...interface{})                  {}
func (l *testRegistryLogger) Warnf(format string, args ...interface{})  {}
func (l *testRegistryLogger) Warnln(args ...interface{})                {}
func (l *testRegistryLogger) WithError(err error) *logrus.Entry {
	panic("TODO somehow get rid of the logrus dependency, used only for test registry setup")
}

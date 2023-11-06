package contain_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	"github.com/distribution/distribution/v3/registry"
	_ "github.com/distribution/distribution/v3/registry/auth/htpasswd"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/phayes/freeport"
	"github.com/sirupsen/logrus"
)

const (
	baseImageNoattest = "test/baseimages/multiarch-test-noattest.oci/"
)

// testRegistry is the host:port to use as registry host for image URLs
var testRegistry string

// testCraneOptions to be used for assertions and such
var testCraneOptions = &crane.Options{}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := setupRegistryServer(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to start docker registry: %s", err))
	}

	err = loadBaseImages()
	if err != nil {
		panic(fmt.Sprintf("failed to load base images: %s", err))
	}

	code := m.Run()
	os.Exit(code)
}

func setupRegistryServer(ctx context.Context) error {
	config := &configuration.Configuration{}
	config.Log.AccessLog.Disabled = true
	config.Log.Level = "error"
	logger := NewTestRegistryLogger()
	dcontext.SetDefaultLogger(logger)
	port, err := freeport.GetFreePort()
	if err != nil {
		return fmt.Errorf("failed to get free port: %s", err)
	}

	testRegistry = fmt.Sprintf("localhost:%d", port)
	config.HTTP.Addr = fmt.Sprintf("127.0.0.1:%d", port)
	config.HTTP.DrainTimeout = time.Duration(10) * time.Second
	config.Storage = map[string]configuration.Parameters{"inmemory": map[string]interface{}{}}
	dockerRegistry, err := registry.NewRegistry(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create docker registry: %w", err)
	}

	go dockerRegistry.ListenAndServe()

	return nil
}

func loadBaseImages() error {
	return loadBaseImage(
		baseImageNoattest,
		fmt.Sprintf("%s/contain-test/multiarch-base", testRegistry),
		"sha256:5df9572dfc5f15f997d84d002274cda07ba5e10d80b667fdd788f9abb9ebf15a",
	)
}

func loadBaseImage(path string, image string, digest string) error {
	abspath, err := filepath.Abs("../../" + path)
	if err != nil {
		return fmt.Errorf("absolute path for %s: %w", path, err)
	}
	img, err := layout.ImageIndexFromPath(abspath)
	if err != nil {
		return fmt.Errorf("loading %s as OCI layout: %w", path, err)
	}
	ref, err := name.ParseReference(image, testCraneOptions.Name...)
	if err != nil {
		return err
	}
	var h v1.Hash
	switch t := img.(type) {
	case v1.ImageIndex:
		if err := remote.WriteIndex(ref, t, testCraneOptions.Remote...); err != nil {
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

type TestRegistryLogger struct {
}

func NewTestRegistryLogger() *TestRegistryLogger {
	return &TestRegistryLogger{}
}

// https://github.com/distribution/distribution/blob/v2.8.3/context/logger.go#L12
func (l *TestRegistryLogger) Print(args ...interface{})                 {}
func (l *TestRegistryLogger) Printf(format string, args ...interface{}) {}
func (l *TestRegistryLogger) Println(args ...interface{})               {}
func (l *TestRegistryLogger) Fatal(args ...interface{})                 {}
func (l *TestRegistryLogger) Fatalf(format string, args ...interface{}) {}
func (l *TestRegistryLogger) Fatalln(args ...interface{})               {}
func (l *TestRegistryLogger) Panic(args ...interface{})                 {}
func (l *TestRegistryLogger) Panicf(format string, args ...interface{}) {}
func (l *TestRegistryLogger) Panicln(args ...interface{})               {}
func (l *TestRegistryLogger) Debug(args ...interface{})                 {}
func (l *TestRegistryLogger) Debugf(format string, args ...interface{}) {}
func (l *TestRegistryLogger) Debugln(args ...interface{})               {}
func (l *TestRegistryLogger) Error(args ...interface{})                 {}
func (l *TestRegistryLogger) Errorf(format string, args ...interface{}) {}
func (l *TestRegistryLogger) Errorln(args ...interface{})               {}
func (l *TestRegistryLogger) Info(args ...interface{})                  {}
func (l *TestRegistryLogger) Infof(format string, args ...interface{})  {}
func (l *TestRegistryLogger) Infoln(args ...interface{})                {}
func (l *TestRegistryLogger) Warn(args ...interface{})                  {}
func (l *TestRegistryLogger) Warnf(format string, args ...interface{})  {}
func (l *TestRegistryLogger) Warnln(args ...interface{})                {}
func (l *TestRegistryLogger) WithError(err error) *logrus.Entry {
	panic("TODO somehow get rid of the logrus dependency, used only for test registry setup")
}

package contain_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/turbokube/contain/pkg/testcases"
)

// testRegistry is the host:port to use as registry host for image URLs
var testRegistry string

// testRegistryLoadBaseimages is false because loading from tar or OCI to multi-arch was tricky
var testRegistryLoadBaseimages = false

// testCraneOptions to be used for assertions and such
var testCraneOptions *crane.Options

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := testcases.NewTestregistry(ctx)

	if err := r.Start(); err != nil {
		panic(fmt.Sprintf("failed to start docker registry: %s", err))
	}

	if testRegistryLoadBaseimages {
		if err := r.LoadBaseImages(); err != nil {
			panic(fmt.Sprintf("failed to load base images: %s", err))
		}
	}

	// these package vars were used by the first generation of tests
	// but we could expose the registry instance instead
	testRegistry = r.Host
	testCraneOptions = &r.Config.CraneOptions

	// Provide deterministic base image name annotation independent of ephemeral registry port.
	// Updated to localhost:12345 to regenerate stable expected digests.
	os.Setenv("CONTAIN_ANNOTATIONS_BASE_REGISTRY_HOST_OVERRIDE", "localhost:12345")

	code := m.Run()
	os.Exit(code)
}

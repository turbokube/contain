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

	r, err := testcases.NewTestregistry(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to init test registry: %s", err))
	}

	err = r.Start()
	if err != nil {
		panic(fmt.Sprintf("failed to start docker registry: %s", err))
	}

	if testRegistryLoadBaseimages {
		err = r.LoadBaseImages()
		if err != nil {
			panic(fmt.Sprintf("failed to load base images: %s", err))
		}
	}

	// these package vars were used by the first generation of tests
	// but we could expose the registry instance instead
	testRegistry = r.Host
	testCraneOptions = r.CraneOptions

	code := m.Run()
	os.Exit(code)
}

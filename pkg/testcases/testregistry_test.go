package testcases

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	testRunDurationEnv = "TEST_REGISTRY_RUN"
)

func TestTestRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := NewTestregistry(ctx)

	if err := r.Start(); err != nil {
		t.Fatalf("testregistry start %v", err)
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/v2/", r.Host))
	if err != nil {
		t.Error(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	if string(body[:]) != "{}" {
		t.Errorf("unexpected response at /v2/: %s", body)
	}

	run, exists := os.LookupEnv(testRunDurationEnv)
	if exists {
		d, err := time.ParseDuration(run)
		if err != nil {
			t.Errorf("parse duration %s=%s", testRunDurationEnv, run)
		}
		t.Logf("Running test registry for %v, then exiting", d)
		t.Logf("Test registry host: %s", r.Host)
		time.Sleep(d)
		return
	}

	// Now check the "noattest" base image
	image := fmt.Sprintf("%s/contain-test/multiarch-base:noattest", r.Host)
	digest, err := crane.Digest(image)
	if err != nil {
		t.Error(err)
	}
	// crane digest solsson/multiarch-test:noattest
	if digest != "sha256:ad170cac387bea5246c9b85f60077b02ebf814d8b151568ad0d35c9b09097b74" {
		t.Errorf("Unexpected base image digest %s", digest)
	}
	// https://github.com/google/go-containerregistry/blob/dbcd01c402b2f05bcf6fb988014c5f37e9b13559/pkg/v1/remote/descriptor.go#L97

	ref, err := name.ParseReference(image, r.Config.CraneOptions.Name...)
	if err != nil {
		t.Error(err)
	}

	amd64 := v1.Platform{Architecture: "amd64", OS: "linux"}
	amd64options := append(r.Config.CraneOptions.Remote, remote.WithPlatform(amd64))
	amd64img, err := remote.Image(ref, amd64options...)
	if err != nil {
		t.Error(err)
	}
	amd64config, err := amd64img.ConfigFile()
	if err != nil {
		t.Error(err)
	}
	if amd64config.History[1].CreatedBy != "COPY ./amd64 / # buildkit" {
		t.Errorf("Unexpected amd64 config %v", amd64config)
	}

	arm64 := v1.Platform{Architecture: "arm64", OS: "linux"}
	arm64options := append(r.Config.CraneOptions.Remote, remote.WithPlatform(arm64))
	arm64img, err := remote.Image(ref, arm64options...)
	if err != nil {
		t.Error(err)
	}
	arm64config, err := arm64img.ConfigFile()
	if err != nil {
		t.Error(err)
	}
	if arm64config.History[1].CreatedBy != "COPY ./arm64 / # buildkit" {
		t.Errorf("Unexpected arm64 config %v", arm64config)
	}
}

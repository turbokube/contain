package testcases

import (
	"context"
	"os"
	"strings"
	"testing"

	"dagger.io/dagger"
)

var client *dagger.Client

type DaggerExpect func(ref string, client *dagger.Client, t *testing.T, ctx context.Context)

func DaggerRef(ref string) string {
	return strings.Replace(ref, "localhost:", "host.docker.internal:", 1)
}

func DaggerClient(t *testing.T, ctx context.Context) *dagger.Client {
	if client == nil {
		var err error
		client, err = dagger.Connect(ctx, dagger.WithLogOutput(os.Stdout))
		if err != nil {
			t.Error(err)
		}
		t.Cleanup(func() {
			client.Close()
		})
	}
	return client
}

package testcases

import (
	"testing"

	"dagger.io/dagger"
)

type DaggerExpect func(ref string, client *dagger.Client, t *testing.T)

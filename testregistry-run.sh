#!/bin/bash
set -e

echo "Note that Docker for Mac buildx build won't be able to push to this registry, as it runs in a buildkit container"

TEST_REGISTRY_RUN=5m go test -v ./pkg/testcases/testregistry.go ./pkg/testcases/testregistry_test.go

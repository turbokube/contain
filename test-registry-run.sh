#!/bin/bash
set -e
TEST_REGISTRY_RUN=5m go test -v ./pkg/contain/main_test.go ./pkg/contain/test_registry_test.go

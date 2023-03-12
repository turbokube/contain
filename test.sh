#!/usr/bin/env bash
[ -z "$DEBUG" ] || set -x
set -eo pipefail

DOCKER=docker
REGISTRY_PORT=22500
REGISTRY_NAME=contain-test-registry

$DOCKER inspect $REGISTRY_NAME 2>/dev/null >/dev/null ||
  $DOCKER run --rm -d -p 22500:5000 --name $REGISTRY_NAME registry:2

mkdir -p dist
go build -o dist/contain-test cmd/contain/main.go

skaffold --default-repo=localhost:22500 -f skaffold.test.yaml build

$DOCKER stop $REGISTRY_NAME

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

skaffold --default-repo=localhost:22500 -f skaffold.test.yaml build --file-output=dist/test.artifacts

skaffold -f skaffold.test.yaml test -a dist/test.artifacts

for F in $(find test -name skaffold.fail-\*.yaml); do
  echo "=> Fail test: $F ..."
  RESULT=0
  skaffold --default-repo=localhost:22500 -f $F build > $F.out 2>&1 || RESULT=$?
  [ $RESULT -eq 0 ] && echo "Expected build failure with $F, got exit $RESULT after:" && cat $F.out && exit 1
  echo "ok"
done

# $DOCKER stop $REGISTRY_NAME

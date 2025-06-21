#!/usr/bin/env bash
[ -z "$DEBUG" ] || set -x
set -eo pipefail

# additional args are passed to skaffold build
# to build a subset use for example: -b config-override,config-stdin

go test ./pkg/...

DOCKER=docker
REGISTRY_PORT=22500
REGISTRY_NAME=contain-test-registry
DEFAULT_REPO=--default-repo=localhost:$REGISTRY_PORT

$DOCKER inspect $REGISTRY_NAME 2>/dev/null >/dev/null ||
  $DOCKER run --rm -d -p 22500:5000 --name $REGISTRY_NAME registry:2

mkdir -p dist/test
go build -ldflags="-X main.BUILD=test-$(uname -m)" -o dist/test/contain cmd/contain/main.go
export PATH=$(pwd)/dist/test:$PATH

contain --version 2>&1 | grep '^test-'
contain --help



skaffold $DEFAULT_REPO -f skaffold.test.yaml build --file-output=dist/test.artifacts --cache-artifacts=false $@

skaffold -f skaffold.test.yaml test -a dist/test.artifacts

# test hacks for things that container-structure-test doesn't (?) support
localtest1=$(cat dist/test.artifacts | jq -r '.builds | .[] | select(.imageName=="localdir1") | .tag')
localtest1_amd64=$(crane --platform=linux/amd64 digest $localtest1)
localtest1_arm64=$(crane --platform=linux/arm64 digest $localtest1)
[ -z "$localtest1_amd64" ] && echo "amd64 architecture missing for $localtest1" && exit 1
[ -z "$localtest1_arm64" ] && echo "arm64 architecture missing for $localtest1" && exit 1
[ "$localtest1_amd64" != "$localtest1_arm64" ] && echo "warning: amd64 != arm64 ($localtest1_amd64 != $localtest1_arm64)" || echo "ok: $localtest1 is multi-arch"

# The original annotations mechanism caused build non-reproducibility due to inclusion of varying registry hostname
# localtest1_base=$(crane manifest $localtest1 | jq -r '.annotations."org.opencontainers.image.base.name"')
# [ "$localtest1_base" = "/library/busybox:latest" ] || (echo "unexpected annotations $localtest1_base" && exit 1)

[ $(cat test/example-stdout.out | wc -l) -eq 1 ] || {
  echo "Error: stdout should be a single line, the image with digest. Got:"
  cat test/example-stdout.out
  exit 1
}

# Test buildctl metadata files
echo "=> Testing buildctl metadata files..."
for buildctl_file in test/out/localdir.buildctl.json test/out/localdir-app.buildctl.json test/out/contextdir-app.buildctl.json; do
  [ -f "$buildctl_file" ] || {
    echo "Error: buildctl metadata file $buildctl_file was not created"
    exit 1
  }

  # Check that it's valid JSON
  jq . "$buildctl_file" > /dev/null || {
    echo "Error: $buildctl_file is not valid JSON"
    exit 1
  }

  # Check that required fields are present
  containerimage_digest=$(jq -r '."containerimage.digest"' "$buildctl_file")
  image_name=$(jq -r '."image.name"' "$buildctl_file")

  [ "$containerimage_digest" != "null" ] && [ "$containerimage_digest" != "" ] || {
    echo "Error: $buildctl_file missing containerimage.digest"
    exit 1
  }

  [ "$image_name" != "null" ] && [ "$image_name" != "" ] || {
    echo "Error: $buildctl_file missing image.name"
    exit 1
  }

  echo "ok: $buildctl_file has valid buildctl metadata"
done

for F in $(find test -name skaffold.fail-\*.yaml); do
  echo "=> Fail test: $F ..."
  RESULT=0
  if [ $F = "test/skaffold.fail-push.yaml" ]; then
    # is there a better way to provoke a push error?
    skaffold -f $F build > $F.out 2>&1 || RESULT=$?
  else
    skaffold $DEFAULT_REPO -f $F build > $F.out 2>&1 || RESULT=$?
  fi
  [ $RESULT -eq 0 ] && echo "Expected build failure with $F, got exit $RESULT after:" && cat $F.out && exit 1
  echo "ok"
done

./test-k8s.sh

echo "All tests passed"

# $DOCKER stop $REGISTRY_NAME

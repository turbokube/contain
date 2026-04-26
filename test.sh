#!/usr/bin/env bash
[ -z "$DEBUG" ] || set -x
set -eo pipefail

# additional args are passed to skaffold build
# to build a subset use for example: -b config-override,config-stdin

mkdir -p dist/test
go test ./pkg/... -coverprofile=dist/test/cover.out -covermode=atomic

# test-k8s.sh creates a k3d cluster; fail early if one already exists
if command -v k3d >/dev/null 2>&1 && k3d cluster list 2>/dev/null | grep -q "^turbokube-test-contain "; then
  echo "ERROR: k3d cluster 'turbokube-test-contain' already exists. Delete it first: k3d cluster delete turbokube-test-contain"
  exit 1
fi

command -v container-structure-test >/dev/null 2>&1 || {
  echo "container-structure-test not found. Install it or use y-container-structure-test from ystack."
  exit 1
}

DOCKER=docker
REGISTRY_PORT=22500
REGISTRY_NAME=contain-test-registry
DEFAULT_REPO=--default-repo=localhost:$REGISTRY_PORT

# Prevent skaffold from detecting local k8s clusters (e.g. k3d) which
# breaks multi-platform builds and container-structure-test image pull.
# Note that test-k8s.sh manages its own KUBECONFIG.
export KUBECONFIG=/dev/null

# Isolate layer cache to test run — do not pollute the user's default cache
export CONTAIN_CACHE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/contain-test-cache.XXXXXX")
echo "Layer cache for this test run: $CONTAIN_CACHE_DIR"

$DOCKER inspect $REGISTRY_NAME 2>/dev/null >/dev/null ||
  $DOCKER run --rm -d -p 22500:5000 --name $REGISTRY_NAME registry:2

TEST_RUN_MODE=""
if [ "$1" = "build-only" ]; then
  TEST_RUN_MODE="build-only"
  shift 1
fi

mkdir -p dist/test
go build -ldflags="-X main.BUILD=test-$(uname -m)" -o dist/test/contain ./cmd/contain
export PATH=$(pwd)/dist/test:$PATH

contain --version 2>&1 | grep '^test-'
contain --help

skaffold $DEFAULT_REPO -f skaffold.test.yaml build --file-output=dist/test.artifacts --cache-artifacts=false $@
[ "$TEST_RUN_MODE" = "build-only" ] && echo "[TEST_RUN_MODE=$TEST_RUN_MODE] exiting before test runs" && exit 0

skaffold -f skaffold.test.yaml test -a dist/test.artifacts

# test that skaffold render/deploy accepts contain's version of the build-output format
info1="$(jq -r '.builds[0] | .mediaType + " " + .platforms[0] + " " + .platforms[1]' test/out/contextdir-app.json)"
[ "$info1" != "application/vnd.oci.image.index.v1+json linux/amd64 linux/arm64/v8" ] && echo "build output: $info1" && exit 1
# must match the repo in the rawYaml file
skaffold --default-repo=localhost:22500 -f skaffold.render-test.yaml render -a test/out/contextdir-app.json --digest-source=local \
  | grep 'image:' | grep '@sha256:'

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
for buildctl_file in test/out/localdir.buildctl.json test/out/localdir-app.buildctl.json test/out/contextdir-app.buildctl.json test/esbuild-main/target/esbuild-main.buildctl.json; do
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

./test-spdx.sh

./test-oci.sh

# --- Layer cache tests (after OCI tests which populate the cache) ---
echo "=> Testing layer cache..."

# Cache info subcommand
contain cache info 2>/dev/null | grep -q 'Path:' || { echo "Error: cache info missing Path"; exit 1; }
contain cache info 2>/dev/null | grep -q 'Size:' || { echo "Error: cache info missing Size"; exit 1; }
echo "ok: cache info format"

# OCI output builds should have populated the cache
CACHE_COUNT=$(contain cache info 2>/dev/null | grep 'Entries:' | awk '{print $2}')
[ "$CACHE_COUNT" -gt 0 ] || { echo "Error: cache should have entries after OCI tests, got $CACHE_COUNT"; exit 1; }
echo "ok: cache has $CACHE_COUNT entries"

# Verify storage internals: sha256-named compressed blobs
CACHE_LAYERS_DIR=$CONTAIN_CACHE_DIR/layers
for f in "$CACHE_LAYERS_DIR"/sha256:*; do
  [ -f "$f" ] || { echo "Error: no sha256-named files in $CACHE_LAYERS_DIR"; exit 1; }
  fname=$(basename "$f")
  echo "$fname" | grep -qE '^sha256:[0-9a-f]{64}$' || { echo "Error: cache file name not sha256 digest: $fname"; exit 1; }
  file "$f" | grep -qi 'gzip\|zstandard\|data' || { echo "Error: cache file $fname is not a compressed blob"; exit 1; }
done
echo "ok: cache storage internals (sha256 digests, compressed blobs)"

# Opt out: CONTAIN_CACHE=false must not add entries
CACHE_BEFORE=$CACHE_COUNT
IMAGE=localhost:$REGISTRY_PORT/nocache-test:latest CONTAIN_CACHE=false contain build -x \
  -b gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1 \
  --push=false --output=$CONTAIN_CACHE_DIR/nocache-out --format=oci \
  test/localdir-app 2>&1
CACHE_AFTER=$(contain cache info 2>/dev/null | grep 'Entries:' | awk '{print $2}')
[ "$CACHE_AFTER" -eq "$CACHE_BEFORE" ] || { echo "Error: CONTAIN_CACHE=false still wrote to cache ($CACHE_BEFORE -> $CACHE_AFTER)"; exit 1; }
echo "ok: CONTAIN_CACHE=false did not pollute cache"

# Purge --all
contain cache purge --all 2>/dev/null
CACHE_PURGED=$(contain cache info 2>/dev/null | grep 'Entries:' | awk '{print $2}')
[ "$CACHE_PURGED" -eq 0 ] || { echo "Error: purge --all left $CACHE_PURGED entries"; exit 1; }
echo "ok: cache purge --all"

./test-k8s.sh

echo "All tests passed"

# $DOCKER stop $REGISTRY_NAME

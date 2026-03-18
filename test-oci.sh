#!/usr/bin/env bash
set -euo pipefail

echo "=> Testing --tarball output..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

TARBALL_OUT="$TMPDIR/image.tar"

# Build a single-platform image as tarball without pushing
PLATFORMS=linux/amd64 \
IMAGE=localhost:22500/contain-test/tarball-test:latest \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --platforms-env-require \
    --tarball "$TARBALL_OUT" \
    --push=false \
    test/localdir-app

[ -f "$TARBALL_OUT" ] || { echo "FAIL: tarball not created"; exit 1; }

# Verify Docker v2 tarball structure
LISTING=$(tar tf "$TARBALL_OUT")

echo "$LISTING" | grep -q 'manifest.json' || { echo "FAIL: manifest.json missing from tarball"; exit 1; }
echo "$LISTING" | grep -q '\.tar\.gz' || { echo "FAIL: no layer blobs in tarball"; exit 1; }

echo "PASS: tarball output looks good"

echo "=> Testing --output --format=oci ..."

OCI_OUT="$TMPDIR/oci-layout"

PLATFORMS=linux/amd64 \
IMAGE=localhost:22500/contain-test/oci-test:latest \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --platforms-env-require \
    --output "$OCI_OUT" \
    --format=oci \
    --push=false \
    test/localdir-app

[ -d "$OCI_OUT" ] || { echo "FAIL: OCI layout directory not created"; exit 1; }
[ -f "$OCI_OUT/oci-layout" ] || { echo "FAIL: oci-layout file missing"; exit 1; }
[ -f "$OCI_OUT/index.json" ] || { echo "FAIL: index.json missing"; exit 1; }
[ -d "$OCI_OUT/blobs/sha256" ] || { echo "FAIL: blobs/sha256 directory missing"; exit 1; }

# Verify index.json has the ref annotation
REF_ANN=$(jq -r '.manifests[0].annotations["org.opencontainers.image.ref.name"]' "$OCI_OUT/index.json")
[ "$REF_ANN" = "localhost:22500/contain-test/oci-test:latest" ] || { echo "FAIL: unexpected ref annotation: $REF_ANN"; exit 1; }

echo "PASS: OCI layout output looks good"

echo "=> Testing CONTAIN_PUSH=false env..."
PLATFORMS=linux/amd64 \
IMAGE=localhost:22500/contain-test/push-env-test:latest \
CONTAIN_PUSH=false \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --platforms-env-require \
    --output "$TMPDIR/push-env-oci" \
    --format=oci \
    test/localdir-app

[ -d "$TMPDIR/push-env-oci" ] || { echo "FAIL: OCI output not created with CONTAIN_PUSH=false"; exit 1; }
echo "PASS: CONTAIN_PUSH=false works"

echo "=> Testing CONTAIN_OCI_OUTPUT env..."
# CONTAIN_OCI_OUTPUT only supports relative paths, so we use the context dir approach
ENVTEST_DIR="$TMPDIR/env-test-workdir"
mkdir -p "$ENVTEST_DIR"
cp -r test/localdir-app/* "$ENVTEST_DIR/"
pushd "$ENVTEST_DIR" > /dev/null
PLATFORMS=linux/amd64 \
IMAGE=localhost:22500/contain-test/oci-env-test:latest \
CONTAIN_OCI_OUTPUT=target-oci \
CONTAIN_PUSH=false \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --platforms-env-require
popd > /dev/null

[ -d "$ENVTEST_DIR/target-oci" ] || { echo "FAIL: OCI output not created via CONTAIN_OCI_OUTPUT"; exit 1; }
[ -f "$ENVTEST_DIR/target-oci/oci-layout" ] || { echo "FAIL: oci-layout missing from env-driven output"; exit 1; }
[ -f "$ENVTEST_DIR/target-oci/index.json" ] || { echo "FAIL: index.json missing from env-driven output"; exit 1; }
echo "PASS: CONTAIN_OCI_OUTPUT works"

echo "=> Testing CONTAIN_OCI_OUTPUT conflict with --format..."
RESULT=0
CONTAIN_OCI_OUTPUT=target-oci \
IMAGE=localhost:22500/contain-test/conflict:latest \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --format=tarball \
    test/localdir-app 2>&1 || RESULT=$?
[ $RESULT -ne 0 ] || { echo "FAIL: expected error when CONTAIN_OCI_OUTPUT + --format"; exit 1; }
echo "PASS: CONTAIN_OCI_OUTPUT conflict detected"

echo "=> Testing CONTAIN_OCI_OUTPUT conflict with --output..."
RESULT=0
CONTAIN_OCI_OUTPUT=target-oci \
IMAGE=localhost:22500/contain-test/conflict2:latest \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --output "$TMPDIR/explicit" \
    test/localdir-app 2>&1 || RESULT=$?
[ $RESULT -ne 0 ] || { echo "FAIL: expected error when CONTAIN_OCI_OUTPUT + --output"; exit 1; }
echo "PASS: CONTAIN_OCI_OUTPUT + --output conflict detected"

echo "=> Testing TURBO_HASH in file-output..."
TURBO_DIR="$TMPDIR/turbo-workdir"
mkdir -p "$TURBO_DIR"
cp -r test/localdir-app/* "$TURBO_DIR/"
pushd "$TURBO_DIR" > /dev/null
TURBO_FILE_OUT="$TMPDIR/turbo-output.json"
PLATFORMS=linux/amd64 \
IMAGE=localhost:22500/contain-test/turbo-test:latest \
TURBO_HASH=abc123def456 \
CONTAIN_PUSH=false \
CONTAIN_OCI_OUTPUT=target-oci \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --platforms-env-require \
    --file-output="$TURBO_FILE_OUT"
popd > /dev/null

TURBO_VAL=$(jq -r '.turborepo.hash' "$TURBO_FILE_OUT")
[ "$TURBO_VAL" = "abc123def456" ] || { echo "FAIL: turborepo.hash=$TURBO_VAL, expected abc123def456"; exit 1; }
echo "PASS: TURBO_HASH appears in file-output"

echo "=> Testing TURBO_HASH absent when env not set..."
NO_TURBO_DIR="$TMPDIR/no-turbo-workdir"
mkdir -p "$NO_TURBO_DIR"
cp -r test/localdir-app/* "$NO_TURBO_DIR/"
pushd "$NO_TURBO_DIR" > /dev/null
NO_TURBO_FILE_OUT="$TMPDIR/no-turbo-output.json"
PLATFORMS=linux/amd64 \
IMAGE=localhost:22500/contain-test/no-turbo-test:latest \
CONTAIN_PUSH=false \
CONTAIN_OCI_OUTPUT=target-oci \
  contain build \
    -b mirror.gcr.io/library/busybox@sha256:c3839dd800b9eb7603340509769c43e146a74c63dca3045a8e7dc8ee07e53966 \
    --platforms-env-require \
    --file-output="$NO_TURBO_FILE_OUT"
popd > /dev/null

TURBO_KEY=$(jq 'has("turborepo")' "$NO_TURBO_FILE_OUT")
[ "$TURBO_KEY" = "false" ] || { echo "FAIL: turborepo should be absent, got $(cat $NO_TURBO_FILE_OUT)"; exit 1; }
echo "PASS: turborepo omitted when TURBO_HASH not set"

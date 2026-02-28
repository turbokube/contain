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

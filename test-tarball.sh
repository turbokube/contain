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

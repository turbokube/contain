#!/usr/bin/env bash
set -euo pipefail

# Test buildctl metadata files
echo "=> Testing buildctl metadata files..."
for buildctl_file in \
    test/out/localdir.buildctl.json \
    test/out/localdir-app.buildctl.json \
    test/out/contextdir-app.buildctl.json \
    test/esbuild-main/target/esbuild-main.buildctl.json \
    ; do
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

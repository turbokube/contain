#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   ./assert-spdx-images.sh <spdx.json|-stdin-> [BASE_NAME] [RESULT_PREFIX]
#
# Defaults match your example:
#   BASE_NAME     = example.net/misc/base-image:abc
#   RESULT_PREFIX = example.net/misc/result-image:cde
#
# RESULT image must look like:  RESULT_PREFIX@sha256:<64-hex>
#
# Notes:
# - If first arg is "-" or omitted, the script reads JSON from stdin
# - Every assertion performs a single jq extraction and then a Bash compare

EXAMPLE1=test/esbuild-main/
EXAMPLE1_BUILD=$EXAMPLE1/target/esbuild-main.json
EXAMPLE1_IN=$EXAMPLE1/target/spdx.json
EXAMPLE1_OUT=$EXAMPLE1/target/spdx-out.json

# Print human-friendly modification date for the build artifacts file
if [[ -f "$EXAMPLE1_BUILD" ]]; then
  if [[ "$(uname)" == "Darwin" ]]; then
    MOD_TIME=$(stat -f "%Sm" -t "%Y-%m-%d %H:%M:%S %Z" "$EXAMPLE1_BUILD")
  else
    MOD_TIME=$(stat -c "%y" "$EXAMPLE1_BUILD")
  fi
  echo "Build artifacts file modified: $MOD_TIME ($EXAMPLE1_BUILD)"
else
  echo "Build artifacts file not found: $EXAMPLE1_BUILD" >&2
fi

cat "$EXAMPLE1_BUILD" | jq '.'

contain sbom --build-artifacts "$EXAMPLE1_BUILD" --in "$EXAMPLE1_IN" --out "$EXAMPLE1_OUT"

cat "$EXAMPLE1_OUT" | jq '.'

if command -v trivy; then
   echo "Found trivy CLI:"
   trivy --version
   # Trivy SBOM scan should reflect that this SBOM represents an application deliverable that's a container image.
   # While Trivy prints 'Node.js' as Target for language analysis, we assert the presence of our container result name in the SBOM.
   TRIVY_JSON=$(TRIVY_NO_PROGRESS=1 trivy sbom --format json "$EXAMPLE1_OUT" || true)
   echo "$TRIVY_JSON" | jq '{ArtifactName, ArtifactType, ResultTargets: (.Results|map(.Target))}'
   # Basic assertion: the input is recognized as an SPDX SBOM by Trivy
   TRIVY_TYPE=$(echo "$TRIVY_JSON" | jq -r '.ArtifactType // ""')
   if [[ "$TRIVY_TYPE" != "spdx" ]]; then
     echo "FAIL: Trivy did not recognize SPDX SBOM (ArtifactType=$TRIVY_TYPE)"
     exit 1
   fi
fi

# Assertions for SPDX enrichment
FILE="$EXAMPLE1_OUT"

# Expected pushed image name@digest from build artifacts
RESULT_TAG=$(jq -r '.builds[0].tag' "$EXAMPLE1_BUILD")
RESULT_IMAGENAME=$(jq -r '.builds[0].imageName' "$EXAMPLE1_BUILD")
RESULT_DIGEST="${RESULT_TAG##*@}"
EXPECT_RESULT_NAME="$RESULT_IMAGENAME@$RESULT_DIGEST"

# 1) Result package exists (exact imageName@sha256:64hex) and is documentDescribes
IMG_NAME="$(jq -r --arg exp "$EXPECT_RESULT_NAME" '.packages[]? | select(.primaryPackagePurpose=="CONTAINER" and .name==$exp) | .name' "$FILE" | head -n1)"
if [[ -z "$IMG_NAME" ]]; then echo "FAIL: result image container package with digest not found"; exit 1; fi
RESULT_ID="$(jq -r --arg n "$IMG_NAME" 'first(.packages[]? | select(.name==$n) | .SPDXID) // ""' "$FILE")"
if [[ -z "$RESULT_ID" ]]; then echo "FAIL: SPDXID for result image not found"; exit 1; fi
if ! jq -e --arg id "$RESULT_ID" '.documentDescribes[]? | select(.==$id) | length >= 0' "$FILE" >/dev/null; then
  echo "FAIL: documentDescribes missing result image"; exit 1
fi

# 2) Relationship RESULT DESCENDANT_OF BASE exists and base has SHA256 checksum
BASE_ID="$(jq -r --arg a "$RESULT_ID" 'first(.relationships[]? | select(.relationshipType=="DESCENDANT_OF" and .spdxElementId==$a) | .relatedSpdxElement) // ""' "$FILE")"
if [[ -z "$BASE_ID" ]]; then echo "FAIL: DESCENDANT_OF relationship missing"; exit 1; fi
BASE_PURPOSE="$(jq -r --arg id "$BASE_ID" 'first(.packages[]? | select(.SPDXID==$id) | .primaryPackagePurpose) // ""' "$FILE")"
if [[ "$BASE_PURPOSE" != "CONTAINER" ]]; then echo "FAIL: related base is not a CONTAINER package"; exit 1; fi
BASE_HAS_SHA256="$(jq -r --arg id "$BASE_ID" 'first(.packages[]? | select(.SPDXID==$id) | .checksums[]? | select(.algorithm=="SHA256") | .checksumValue) // ""' "$FILE")"
if ! [[ "$BASE_HAS_SHA256" =~ ^[0-9a-f]{64}$ ]]; then echo "FAIL: base image missing SHA256 checksum"; exit 1; fi

# 3) Base package name matches expected for esbuild-main test
EXPECT_BASE_NAME="gcr.io/distroless/nodejs24-debian12:nonroot"
BASE_NAME_ACTUAL="$(jq -r --arg id "$BASE_ID" 'first(.packages[]? | select(.SPDXID==$id) | .name) // ""' "$FILE")"
if [[ "$BASE_NAME_ACTUAL" != "$EXPECT_BASE_NAME" ]]; then
  echo "FAIL: base image name mismatch"
  echo "  expected: $EXPECT_BASE_NAME"
  echo "  actual:   $BASE_NAME_ACTUAL"
  exit 1
fi

echo "PASS: SPDX enrichment looks good"

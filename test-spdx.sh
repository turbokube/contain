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
   trivy sbom $EXAMPLE1_OUT
fi

# TODO actually assert stuff. Below code is from an early exploration.
echo "Specific assertions (unmaintained) skipped until in scope" && exit 0

BASE_NAME="${2:-example.net/misc/base-image:abc}"
RESULT_PREFIX="${3:-example.net/misc/result-image:cde}"

# ---- 1) Base Package exists (exact name) ----
BASE_ID="$(
  jq -r --arg base "$BASE_NAME" '
    first(.packages[]?
      | select(.primaryPackagePurpose=="CONTAINER" and .name==$base)
      | .SPDXID) // ""
  ' "$FILE"
)"
if [[ -z "$BASE_ID" ]]; then
  echo "FAIL: base image package not found"
  echo "  expected name: $BASE_NAME"
  echo "  actual container package names:"
  jq -r '.packages[]? | select(.primaryPackagePurpose=="CONTAINER") | "    - \(.name)"' "$FILE"
  exit 1
fi

# ---- 2) Result Package exists (name starts with prefix@ and has sha256:64hex) ----
RESULT_NAME="$(
  jq -r --arg pref "$RESULT_PREFIX" '
    first(.packages[]?
      | select(.primaryPackagePurpose=="CONTAINER" and (.name | startswith($pref+"@")))
      | .name) // ""
  ' "$FILE"
)"
if [[ -z "$RESULT_NAME" ]]; then
  echo "FAIL: result image package not found"
  echo "  expected prefix: ${RESULT_PREFIX}@sha256:<64hex>"
  echo "  actual container package names:"
  jq -r '.packages[]? | select(.primaryPackagePurpose=="CONTAINER") | "    - \(.name)"' "$FILE"
  exit 1
fi

# Validate digest format purely in Bash
if [[ "$RESULT_NAME" != "$RESULT_PREFIX@"* ]]; then
  echo "FAIL: result image name does not start with expected prefix"
  echo "  expected prefix: ${RESULT_PREFIX}@"
  echo "  actual name: $RESULT_NAME"
  exit 1
fi
DIGEST="${RESULT_NAME#*@}"
if ! [[ "$DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo "FAIL: result image digest has unexpected format"
  echo "  expected: sha256:<64-hex>"
  echo "  actual:   $DIGEST"
  exit 1
fi

# ---- 3) Resolve RESULT_ID from the exact RESULT_NAME ----
RESULT_ID="$(
  jq -r --arg name "$RESULT_NAME" '
    first(.packages[]? | select(.name==$name) | .SPDXID) // ""
  ' "$FILE"
)"
if [[ -z "$RESULT_ID" ]]; then
  echo "FAIL: could not resolve SPDXID for result image"
  echo "  result name: $RESULT_NAME"
  exit 1
fi

# ---- 4) Relationship: RESULT DESCENDANT_OF BASE ----
# Extract the related element for a DESCENDANT_OF from RESULT; compare to BASE_ID
REL_DESC_TO="$(
  jq -r --arg f "$RESULT_ID" '
    first(.relationships[]?
      | select(.relationshipType=="DESCENDANT_OF" and .spdxElementId==$f)
      | .relatedSpdxElement) // ""
  ' "$FILE"
)"
if [[ "$REL_DESC_TO" != "$BASE_ID" ]]; then
  echo "FAIL: missing/wrong DESCENDANT_OF relationship"
  echo "  expected: $RESULT_ID -> $BASE_ID"
  echo "  actual:   $RESULT_ID -> ${REL_DESC_TO:-<none>}"
  echo "  relationships for $RESULT_ID:"
  jq -r --arg f "$RESULT_ID" '
    .relationships[]?
    | select(.spdxElementId==$f)
    | "    - \(.spdxElementId) \(.relationshipType) \(.relatedSpdxElement)"
  ' "$FILE"
  exit 1
fi

# ---- 5) documentDescribes includes RESULT_ID ----
DESC_MATCH="$(
  jq -r --arg f "$RESULT_ID" '
    first(.documentDescribes[]? | select(.==$f)) // ""
  ' "$FILE"
)"
if [[ "$DESC_MATCH" != "$RESULT_ID" ]]; then
  echo "FAIL: result image not listed in documentDescribes"
  echo "  expected id: $RESULT_ID"
  echo "  actual list:"
  jq -r '.documentDescribes[]? | "    - " + .' "$FILE"
  exit 1
fi

echo "PASS"
echo "  BASE_ID=$BASE_ID  BASE_NAME=$BASE_NAME"
echo "  RESULT_ID=$RESULT_ID  RESULT_NAME=$RESULT_NAME"

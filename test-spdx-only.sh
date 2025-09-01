#!/usr/bin/env bash
[ -z "$DEBUG" ] || set -x
set -eo pipefail

./test.sh build-only -b esbuild-main

# use the contain build from test.sh
export PATH=$(pwd)/dist/test:$PATH

./test-spdx.sh

#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
nim_bin="$(readlink -f "$(command -v nim)")"
nim_lib="$(dirname "$(dirname "$nim_bin")")/lib"
patch --batch --forward -p1 -d "$nim_lib" \
  < tools/nim-httpclient-backpressure.patch

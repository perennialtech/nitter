#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
nim_bin="$(readlink -f "$(command -v nim)")"
nim_root="$(dirname "$(dirname "$nim_bin")")"
nim_lib="$nim_root/lib"
command -v patch >/dev/null || { echo "The patch utility is required" >&2; exit 1; }

httpclient="$nim_lib/pure/httpclient.nim"
asyncdispatch="$nim_lib/pure/asyncdispatch.nim"
patch_file=tools/nim-httpclient-backpressure.patch

# Every hunk has a distinctive signature. GNU patch's reverse detection can
# silently apply a partial patch again, so check all signatures explicitly.
marker=$(grep -Fc 'const nitterAsyncSocketOwnershipPatched* = true' "$httpclient" || true)
progress=$(grep -Fc '# Await the callback for each received block' "$httpclient" || true)
connect=$(grep -Fc 'await client.socket.connect(connectionUrl.hostname, port)' "$httpclient" || true)
dispatch=$(grep -Fc 'closeSocket invokes pending callbacks after closing the descriptor.' "$asyncdispatch" || true)

if (( marker == 1 && progress == 1 && connect == 1 && dispatch == 1 )); then
  # Check every added and removed line, not just the identifying signatures.
  if ! patch --dry-run --reverse --force --fuzz=0 -p1 -d "$nim_lib" \
    < "$patch_file" >/dev/null 2>&1; then
    echo "Nim HTTP transport patch is incomplete or modified" >&2
    exit 1
  fi
  echo "Nim HTTP transport patch is already applied"
elif (( marker + progress + connect + dispatch > 0 )); then
  echo "Nim HTTP transport patch is only partially applied" >&2
  exit 1
else
  patch --dry-run --batch --forward --fuzz=0 -p1 -d "$nim_lib" \
    < "$patch_file" >/dev/null
  patch --batch --forward --fuzz=0 -p1 -d "$nim_lib" < "$patch_file"
fi

# Nim's ARC/ORC compiles `return x` as a bitwise copy into `result`, leaking
# any value assigned to `result` earlier (packedjson's `{}` does this for
# every missing key, retaining the whole parsed response).
compiler_patch=tools/nim-return-destroy.patch
if patch --dry-run --reverse --force --fuzz=0 -p1 -d "$nim_root" \
  < "$compiler_patch" >/dev/null 2>&1; then
  echo "Nim compiler patch is already applied"
elif patch --dry-run --batch --forward --fuzz=0 -p1 -d "$nim_root" \
  < "$compiler_patch" >/dev/null 2>&1; then
  patch --batch --forward --fuzz=0 -p1 -d "$nim_root" < "$compiler_patch"
else
  echo "Nim compiler patch is only partially applied or does not apply" >&2
  exit 1
fi

# The patched sources take effect only in a rebuilt compiler.
if ! nim dump 2>&1 | grep -qx nitterReturnDestroyPatched; then
  (cd "$nim_root" && nim c -d:release --skipUserCfg --skipParentCfg \
    --hints:off --warnings:off -o:"$nim_root/bin/nim.patched" compiler/nim.nim)
  mv "$nim_root/bin/nim.patched" "$nim_bin"
fi

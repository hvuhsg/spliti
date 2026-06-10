#!/usr/bin/env bash
# Builds the GPU demos to WebAssembly and stages the web/ harness.
#
# Output goes to web/dist/: one <demo>.wasm per demo plus the toolchain's
# wasm_exec.js. Serve the result with `go run ./cmd/webserve` and open
# http://localhost:8080/ in a WebGPU-capable browser.
set -euo pipefail

cd "$(dirname "$0")/.."

DIST=web/dist
DEMOS=(gpu-demo render3d-demo)

mkdir -p "$DIST"

# wasm_exec.js must match the Go toolchain that built the .wasm.
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$DIST/wasm_exec.js"

for demo in "${DEMOS[@]}"; do
  echo "building $demo -> $DIST/$demo.wasm"
  GOOS=js GOARCH=wasm go build -o "$DIST/$demo.wasm" "./examples/$demo"
done

echo
echo "Done. Now run:  go run ./cmd/webserve"
echo "Then open:      http://localhost:8080/?demo=gpu-demo"

#!/usr/bin/env bash
# Build a spliti GPU example into a SINGLE self-contained .html file that runs by
# double-clicking it (opens in your default browser, no server needed).
#
# Everything is inlined: the Go wasm runtime (wasm_exec.js) and the .wasm itself
# (base64) — so there is no fetch(), which a file:// page can't do. WebGPU runs
# because browsers treat file:// as a secure context.
#
# Usage:
#   scripts/build-single.sh [demo] [output.html]
# Examples:
#   scripts/build-single.sh gpu-demo
#   scripts/build-single.sh render3d-demo ~/Desktop/render3d.html
#
# Requires a WebGPU-capable browser (recent Chrome/Edge; Firefox with WebGPU on).
set -euo pipefail

cd "$(dirname "$0")/.."

demo="${1:-gpu-demo}"
out="${2:-$demo.html}"

if [ ! -d "examples/$demo" ]; then
  echo "no such example: examples/$demo" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "building examples/$demo -> wasm ..."
GOOS=js GOARCH=wasm go build -o "$tmp/app.wasm" "./examples/$demo"

wasmexec="$(go env GOROOT)/lib/wasm/wasm_exec.js"

echo "inlining into $out ..."
{
  cat <<'HTML_HEAD'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>spliti</title>
<style>
  html, body { margin: 0; height: 100%; background: #0b0d10; color: #e6e6e6;
    font: 14px/1.4 system-ui, -apple-system, sans-serif; overflow: hidden; }
  #spliti-canvas { display: block; width: 100vw; height: 100vh; outline: none; }
  #msg { position: fixed; inset: 0; display: flex; align-items: center;
    justify-content: center; text-align: center; padding: 2rem; }
  #msg.hidden { display: none; }
  code { color: #ffd479; }
</style>
</head>
<body>
<canvas id="spliti-canvas" tabindex="0"></canvas>
<div id="msg">Loading…</div>
<script>
HTML_HEAD

  # 1) The Go wasm runtime.
  cat "$wasmexec"

  echo '</script>'
  echo '<script>'

  # 2) The wasm binary, base64-inlined (no fetch needed for file://).
  printf 'const wasmBase64 = "'
  base64 < "$tmp/app.wasm" | tr -d '\n'
  printf '";\n'

  # 3) Boot: detached-buffer shim, decode, instantiate, run.
  cat <<'HTML_TAIL'
  // cogentcore/webgpu's js queue writes build a Uint8ClampedArray over Go's
  // wasm memory; if Go grows (and detaches) that memory mid-write the construct
  // throws. Retarget a detached buffer to the live wasm memory at the same
  // offset (growth preserves contents).
  (function () {
    const U8C = globalThis.Uint8ClampedArray;
    globalThis.Uint8ClampedArray = new Proxy(U8C, {
      construct(target, args) {
        const b = args[0];
        if (b instanceof ArrayBuffer && b.byteLength === 0 &&
            globalThis.wasm?.instance?.exports?.mem?.buffer) {
          return new target(globalThis.wasm.instance.exports.mem.buffer, args[1], args[2]);
        }
        return new target(...args);
      },
    });
  })();

  async function main() {
    const msg = document.getElementById("msg");
    if (!navigator.gpu) {
      msg.innerHTML = "This browser does not support <b>WebGPU</b>.<br>" +
        "Use a recent Chrome/Edge, or Firefox with WebGPU enabled.";
      return;
    }
    // Decode the inlined wasm bytes.
    const bin = atob(wasmBase64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);

    const go = new Go();
    try {
      const result = await WebAssembly.instantiate(bytes, go.importObject);
      globalThis.wasm = result; // the lib reads wasm memory through this
      msg.classList.add("hidden");
      document.getElementById("spliti-canvas").focus();
      await go.run(result.instance); // resolves when the game exits
      msg.classList.remove("hidden");
      msg.textContent = "Exited. Reload to run again.";
    } catch (e) {
      msg.innerHTML = "Failed to start: " + e;
      console.error(e);
    }
  }
  main();
HTML_TAIL

  echo '</script>'
  echo '</body>'
  echo '</html>'
} > "$out"

size="$(du -h "$out" | cut -f1)"
echo "wrote $out ($size)"
echo "Double-click it to open in a WebGPU-capable browser."

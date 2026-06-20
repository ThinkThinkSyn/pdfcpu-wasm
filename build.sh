#!/usr/bin/env bash
# Build pdfcpu WASM module (standalone packaging repo)
#
# Prerequisites:
#   - Go 1.25+ (matching go.mod)
#   - Go's js/wasm runtime (bundled with Go)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${SCRIPT_DIR}/dist"

PDFCPU_VERSION="$(cd "${SCRIPT_DIR}" && go list -m -f '{{.Version}}' github.com/pdfcpu/pdfcpu)"

if [ -z "$PDFCPU_VERSION" ]; then
  echo "Error: could not find pdfcpu version via go list" >&2
  exit 1
fi

echo "=== Building pdfcpu WASM ==="
echo "pdfcpu dependency: ${PDFCPU_VERSION}"
echo "Output dir:        ${OUT_DIR}"

mkdir -p "${OUT_DIR}"

# -------------------------------------------------------
# 1. Compile the WASM binary
# -------------------------------------------------------
GOOS=js GOARCH=wasm \
  go build \
  -tags js \
  -trimpath \
  -ldflags="-s -w" \
  -o "${OUT_DIR}/pdfcpu.wasm" \
  "${SCRIPT_DIR}"

echo "  ✓ pdfcpu.wasm built ($(du -h "${OUT_DIR}/pdfcpu.wasm" | cut -f1))"

# -------------------------------------------------------
# 2. Copy the Go WASM executor JavaScript glue
# -------------------------------------------------------
GOROOT="$(go env GOROOT)"
# Go 1.25+ moved the file from misc/wasm/ to lib/wasm/
if [ -f "${GOROOT}/lib/wasm/wasm_exec.js" ]; then
  cp "${GOROOT}/lib/wasm/wasm_exec.js" "${OUT_DIR}/wasm_exec.js"
else
  cp "${GOROOT}/misc/wasm/wasm_exec.js" "${OUT_DIR}/wasm_exec.js"
fi
echo "  ✓ wasm_exec.js copied from Go ${GOROOT}"

# -------------------------------------------------------
# 3. Generate a version stamp
# -------------------------------------------------------
cat > "${SCRIPT_DIR}/version.json" <<VEOF
{
  "pdfcpu": "${PDFCPU_VERSION}",
  "built": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "commit": "$(git -C "${SCRIPT_DIR}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
}
VEOF
echo "  ✓ version.json created"

# -------------------------------------------------------
# 4. Print summary
# -------------------------------------------------------
echo ""
echo "=== Output ==="
ls -lh "${OUT_DIR}/"
echo ""
echo "Next steps:"
echo "  1. Serve ${OUT_DIR}/ as static files (or copy into your TS project)"
echo "  2. Import pdfcpu.ts from your TypeScript project"
echo "  3. See example/ for usage examples"

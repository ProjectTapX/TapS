#!/usr/bin/env bash
# Cross-compile panel and daemon binaries into ./dist for common targets.
# Web bundle is built into web/dist by `npm run build`.
set -euo pipefail
cd "$(dirname "$0")/.."

export GOFLAGS="-trimpath"
LDFLAGS='-s -w'

mkdir -p dist
TARGETS=(
  "linux amd64"
  "linux arm64"
)

build() {
  local pkg=$1
  for t in "${TARGETS[@]}"; do
    read -r OS ARCH <<<"$t"
    ext=""; [[ "$OS" == "windows" ]] && ext=".exe"
    out="dist/${pkg}-${OS}-${ARCH}${ext}"
    echo "==> ${out}"
    GOOS=$OS GOARCH=$ARCH CGO_ENABLED=0 \
      go -C "packages/${pkg}" build -ldflags "$LDFLAGS" -o "../../${out}" "./cmd/${pkg}"
  done
}

build panel
build daemon

echo "==> web"
( cd web && npm install --silent && npm run build )
cp -R web/dist dist/web

echo "All artifacts written to ./dist"

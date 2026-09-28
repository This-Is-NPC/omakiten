#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

mkdir -p .tmp/tests
targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 plan9/amd64)
if [[ -n "${usage_target:-${1:-}}" ]]; then
  targets=("${usage_target:-$1}")
fi
for target in "${targets[@]}"; do
  packages=(config paths)
  if [[ "${target%/*}" != plan9 ]]; then
    packages+=(sqlite)
  fi
  for package in "${packages[@]}"; do
    printf 'Compiling %s for %s\n' "$package" "$target"
    GOOS="${target%/*}" GOARCH="${target#*/}" go test -c \
      -o ".tmp/tests/${package}-${target%/*}-${target#*/}.test" "./internal/$package"
  done
done

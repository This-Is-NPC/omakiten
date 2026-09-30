#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 plan9/amd64)
if [[ -n "${usage_target:-${1:-}}" ]]; then
  targets=("${usage_target:-$1}")
fi
for target in "${targets[@]}"; do
  case "$target" in
    linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64|windows/arm64|plan9/amd64) ;;
    *) printf 'Unsupported compile target: %s\n' "$target" >&2; exit 2 ;;
  esac
  mkdir -p .tmp/tests
  packages=(config paths)
  if [[ "${target%/*}" != plan9 ]]; then
    packages+=(sqlite filelock)
  fi
  for package in "${packages[@]}"; do
    printf 'Compiling %s for %s\n' "$package" "$target"
    GOOS="${target%/*}" GOARCH="${target#*/}" go test -c \
      -o ".tmp/tests/${package}-${target%/*}-${target#*/}.test" "./internal/$package"
  done
done

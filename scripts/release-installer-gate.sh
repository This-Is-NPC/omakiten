#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: release-installer-gate.sh <v-tag> <dist-dir>" >&2
  exit 2
fi

tag="$1"
case "$tag" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "error: release tag must start with v and contain a semantic version" >&2; exit 2 ;;
esac
version="${tag#v}"
repo="This-Is-NPC/omakiten"
asset="okt_Linux_x86_64.tar.gz"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="$(cd "$2" && pwd)"

for tool in cosign curl jq python3 pwsh; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "error: release installer gate requires $tool" >&2
    exit 1
  }
done

tmpdir="$(mktemp -d)"
server_pid=""
cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

release_dir="$tmpdir/www/$repo/releases/download/$tag"
mkdir -p "$release_dir"
required=(
  "$asset"
  checksums.txt
  "release-manifest-${tag}.json"
  "release-manifest-${tag}.sigstore.json"
  "checksums-${tag}.sigstore.json"
  "release-provenance-${tag}.sigstore.json"
)
for name in "${required[@]}"; do
  test -f "$dist/$name"
  cp "$dist/$name" "$release_dir/$name"
done

port_file="$tmpdir/port"
python3 - "$tmpdir/www" "$port_file" <<'PY' &
import http.server
import pathlib
import sys

root, port_file = sys.argv[1:]
handler = lambda *args, **kwargs: http.server.SimpleHTTPRequestHandler(*args, directory=root, **kwargs)
server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
pathlib.Path(port_file).write_text(str(server.server_port), encoding="ascii")
server.serve_forever()
PY
server_pid="$!"
for _ in $(seq 1 100); do
  [ -s "$port_file" ] && break
  kill -0 "$server_pid" 2>/dev/null || {
    echo "error: local release server exited before becoming ready" >&2
    exit 1
  }
  sleep 0.05
done
test -s "$port_file"

port="$(<"$port_file")"
case "$port" in
  ''|*[!0-9]*) echo "error: local release server returned invalid port '$port'" >&2; exit 1 ;;
esac
if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
  echo "error: local release server returned out-of-range port '$port'" >&2
  exit 1
fi
GITHUB_DL_BASE="http://127.0.0.1:$port"
export GITHUB_DL_BASE
export OKT_INSTALLER_TEST_LIBRARY=1
export INSTALL_DIR="$tmpdir/install"
export OKT_GATE_ROOT="$root"
export OKT_GATE_ARCHIVE="$dist/$asset"
export OKT_GATE_ASSET="$asset"
export OKT_GATE_VERSION="$version"
export OKT_GATE_TMP="$tmpdir/powershell"
mkdir -p "$OKT_GATE_TMP" "$tmpdir/bash"

# Source and execute the exact production strict-verification function.
source "$root/install.sh"
COSIGN_BIN="$(command -v cosign)"
verify_release_strict "$dist/$asset" "$asset" "$version" "$tmpdir/bash"
echo "=> Production Bash strict installer verification passed"

# The single-quoted payload is PowerShell source and must not expand in Bash.
# shellcheck disable=SC2016
pwsh -NoProfile -NonInteractive -Command '
  $env:OKT_INSTALLER_TEST_LIBRARY = "1"
  . (Join-Path $env:OKT_GATE_ROOT "install.ps1")
  $cosign = (Get-Command cosign -CommandType Application).Source
  Invoke-StrictVerification -Cosign $cosign -Archive $env:OKT_GATE_ARCHIVE -Asset $env:OKT_GATE_ASSET -Tag $env:OKT_GATE_VERSION -TmpDir $env:OKT_GATE_TMP
'
echo "=> Production PowerShell strict installer verification passed"

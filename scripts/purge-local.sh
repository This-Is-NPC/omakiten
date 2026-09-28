#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

: "${HOME:?HOME is not set}"
rm -rf "$HOME/.config/omakiten" "$HOME/.local/share/omakiten"

#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

if [ -z "${HOME:-}" ]; then
  printf 'HOME is not set\n' >&2
  exit 1
fi
installed_at="$HOME/.local/bin/okt"
install -Dm755 .tmp/build/okt "$installed_at"
"$PWD/scripts/sync-defaults.sh" "$HOME/.config/omakiten"

# Warn when PATH selects another okt installation.
on_path="$(command -v okt 2>/dev/null || true)"
if [ -n "$on_path" ] && [ "$on_path" != "$installed_at" ]; then
  printf '\n\033[33mWARN:\033[0m PATH resolves okt to %s, not %s.\n' "$on_path" "$installed_at" >&2
  printf '       Remove the stale copy or reorder PATH so $HOME/.local/bin wins.\n' >&2
  printf '       e.g. rm %s\n\n' "$on_path" >&2
fi

# Refresh shipped defaults before registering the local project.
if [[ "${1:-}" == --silent ]]; then
  "$installed_at" setup --update --cli-lang en --tui-lang en --agent-lang en --preset omakase --skip-harnesses
else
  "$installed_at" setup --update
fi

"$installed_at" init --name Omakiten --slug omakiten --root "$PWD"

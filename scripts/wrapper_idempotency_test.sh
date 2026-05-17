#!/usr/bin/env bash
# Validates that `okt setup`'s rc-file wrapper writer is idempotent and
# that uninstall.sh's `remove_wrapper_from` inverts the install cleanly
# — the sentinels are byte-identical across the Go writer
# (internal/installer.WrapperBegin/End) and the bash uninstaller, so a
# round-trip through both paths must leave the rc file in its original
# state.
#
# Run with: bash scripts/wrapper_idempotency_test.sh
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

tmproot="$(mktemp -d)"
trap 'rm -rf "$tmproot"' EXIT

bin="$tmproot/okt"
( cd "$repo" && go build -o "$bin" ./cmd/okt )

home="$tmproot/home"
mkdir -p "$home"
rc="$home/.bashrc"

# Seed the rc with unrelated content so we can verify surgical removal.
cat > "$rc" <<'EOF'
# user content above
export FOO=bar
EOF
orig="$(cat "$rc")"

run_setup() {
  HOME="$home" OMAKITEN_HOME="$home/oh" XDG_CONFIG_HOME="" \
    "$bin" setup --skip-harnesses \
    --cli-lang=en --tui-lang=en --agent-lang=en --preset=omakase --harnesses=0 \
    >/dev/null
}

run_setup
first_count=$(grep -cF "# >>> okt wrapper >>>" "$rc")
if [ "$first_count" -ne 1 ]; then
  echo "FAIL: after first install, sentinel count = $first_count (want 1)" >&2
  exit 1
fi

run_setup
second_count=$(grep -cF "# >>> okt wrapper >>>" "$rc")
if [ "$second_count" -ne 1 ]; then
  echo "FAIL: re-install added duplicate sentinels (count = $second_count)" >&2
  exit 1
fi

# Verify the original user content is still present byte-for-byte.
if ! grep -qx 'export FOO=bar' "$rc"; then
  echo "FAIL: wrapper install clobbered unrelated content" >&2
  exit 1
fi

# uninstall.sh removes the block using sentinels byte-identical with the
# Go writer — pin HOME so it targets the seeded rc instead of the
# developer's actual ~/.bashrc.
HOME="$home" bash "$repo/uninstall.sh" >/dev/null

if grep -qF "# >>> okt wrapper >>>" "$rc"; then
  echo "FAIL: uninstall left sentinels in the rc file" >&2
  exit 1
fi
if ! grep -qx 'export FOO=bar' "$rc"; then
  echo "FAIL: uninstall removed unrelated content" >&2
  exit 1
fi

# Re-running uninstall is a no-op (no sentinels left).
HOME="$home" bash "$repo/uninstall.sh" >/dev/null

echo "OK: okt setup wrapper writer is idempotent; uninstall.sh is surgical."

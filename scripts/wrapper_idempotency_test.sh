#!/usr/bin/env bash
# Validates that install.sh's install_wrapper_into is idempotent and that
# uninstall.sh's remove_wrapper_from inverts the install cleanly.
#
# Run with: bash scripts/wrapper_idempotency_test.sh
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_rc=$(mktemp)
trap 'rm -f "$tmp_rc" "$tmp_rc.bak"' EXIT

# Seed the rc file with some pre-existing content so we can verify removal
# is surgical (does not touch unrelated lines).
cat > "$tmp_rc" <<'EOF'
# user content above
export FOO=bar
EOF
cp "$tmp_rc" "$tmp_rc.bak"

# Source install.sh in a way that exposes the wrapper helpers without
# triggering main(): wrap with a sentinel so the script's "main \"$@\""
# tail does not run. Rather than parsing, source under a guard env var
# the script does not set and dispatch only to the helper we want.
source_helpers() {
  # Extract install_wrapper_into + WRAPPER_BEGIN/END definitions only.
  awk '
    /^WRAPPER_(BEGIN|END)=/ { print; next }
    /^install_wrapper_into\(\) \{/ { in_fn = 1 }
    in_fn { print }
    in_fn && /^\}$/ { in_fn = 0 }
  ' "$repo/install.sh"
}

eval "$(source_helpers)"

install_wrapper_into "$tmp_rc"
first_count=$(grep -cF "$WRAPPER_BEGIN" "$tmp_rc")
if [ "$first_count" -ne 1 ]; then
  echo "FAIL: after first install, sentinel count = $first_count (want 1)" >&2
  exit 1
fi

install_wrapper_into "$tmp_rc"
second_count=$(grep -cF "$WRAPPER_BEGIN" "$tmp_rc")
if [ "$second_count" -ne 1 ]; then
  echo "FAIL: re-install added duplicate sentinels (count = $second_count)" >&2
  exit 1
fi

# Verify uninstall removes the block and leaves the original content intact.
source_uninstall() {
  awk '
    /^WRAPPER_(BEGIN|END)=/ { print; next }
    /^remove_wrapper_from\(\) \{/ { in_fn = 1 }
    in_fn { print }
    in_fn && /^\}$/ { in_fn = 0 }
  ' "$repo/uninstall.sh"
}
eval "$(source_uninstall)"

remove_wrapper_from "$tmp_rc"
if grep -qF "$WRAPPER_BEGIN" "$tmp_rc"; then
  echo "FAIL: uninstall left sentinels in the rc file" >&2
  exit 1
fi
if ! grep -qx 'export FOO=bar' "$tmp_rc"; then
  echo "FAIL: uninstall removed unrelated content" >&2
  exit 1
fi

# Re-running uninstall is a no-op (no sentinels left).
remove_wrapper_from "$tmp_rc"

echo "OK: install_wrapper_into is idempotent; remove_wrapper_from is surgical."

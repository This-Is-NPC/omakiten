package cli

import (
	"path/filepath"
	"testing"
)

// TestBlockedBackupOutRoot pins the system-path allowlist for
// `db backup --out`. Inputs are cleaned absolute paths the runner has
// already resolved via filepath.Abs + filepath.Clean.
func TestBlockedBackupOutRoot(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		wantBlocked bool
		wantRoot    string
	}{
		{"under /etc", filepath.FromSlash("/etc/omakiten.db"), true, "/etc"},
		{"deep under /usr", filepath.FromSlash("/usr/share/omakiten/snap.db"), true, "/usr"},
		{"under /proc", filepath.FromSlash("/proc/self/snap.db"), true, "/proc"},
		{"under /sys", filepath.FromSlash("/sys/devices/snap.db"), true, "/sys"},
		{"under /dev", filepath.FromSlash("/dev/null"), true, "/dev"},
		{"exact /etc", filepath.FromSlash("/etc"), true, "/etc"},
		{"sibling /etcetera does not match", filepath.FromSlash("/etcetera/snap.db"), false, ""},
		{"home dir allowed", filepath.FromSlash("/home/user/snap.db"), false, ""},
		{"tmp allowed", filepath.FromSlash("/tmp/snap.db"), false, ""},
		{"opt allowed", filepath.FromSlash("/opt/omakiten/snap.db"), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, blocked := blockedBackupOutRoot(tc.path)
			if blocked != tc.wantBlocked {
				t.Fatalf("blocked=%v root=%q, want blocked=%v root=%q", blocked, root, tc.wantBlocked, tc.wantRoot)
			}
			if blocked && root != tc.wantRoot {
				t.Fatalf("matched root=%q, want %q", root, tc.wantRoot)
			}
		})
	}
}

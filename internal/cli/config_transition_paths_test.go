package cli

import (
	"path/filepath"
	"testing"
)

func TestValidateV030ManagedConfigPathRejectsNonOfficialPaths(t *testing.T) {
	root := t.TempDir()
	official := filepath.Join(root, "config", "omakase.yaml")
	if got, err := validateManagedConfigProfilePath(official); err != nil || got != official {
		t.Fatalf("absent official path = %q, %v; want acceptance", got, err)
	}

	cases := map[string]string{
		"custom preset":   filepath.Join(root, "config", "custom", "omakase.yaml"),
		"outside config":  filepath.Join(root, "omakase.yaml"),
		"case variant":    filepath.Join(root, "config", "Omakase.yaml"),
		"wrong extension": filepath.Join(root, "config", "omakase.json"),
		"wrong layout":    filepath.Join(root, "config", "nested", "omakase.yaml"),
	}
	for name, path := range cases {
		name, path := name, path
		t.Run(name, func(t *testing.T) {
			assertTransitionPathRejected(t, path)
		})
	}
}

func assertTransitionPathRejected(t *testing.T, path string) {
	t.Helper()
	if _, err := validateManagedConfigProfilePath(path); err == nil {
		t.Fatalf("validateManagedConfigProfilePath(%q) accepted a non-official path", path)
	}
}

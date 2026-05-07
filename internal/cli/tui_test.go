package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestIsProjectNotFoundErrorMatchesCodedDomainError covers the helper that
// runTUI uses to decide whether to swallow a resolver error and open the
// multi-project Home view. Only the project_not_found code should match.
func TestIsProjectNotFoundErrorMatchesCodedDomainError(t *testing.T) {
	t.Run("project_not_found", func(t *testing.T) {
		err := domain.NewError(domain.ErrProjectNotFound, "no match", nil)
		if !isProjectNotFoundError(err) {
			t.Fatalf("project_not_found should match")
		}
	})
	t.Run("other coded error does not match", func(t *testing.T) {
		err := domain.NewError(domain.ErrConfigInvalid, "bad", nil)
		if isProjectNotFoundError(err) {
			t.Fatalf("config_invalid must not match")
		}
	})
	t.Run("plain error does not match", func(t *testing.T) {
		if isProjectNotFoundError(errors.New("io error")) {
			t.Fatalf("plain error must not match")
		}
	})
}

// TestOktCDPathHonorsEnvOverrides documents the resolution order used by
// the cd-on-exit handshake: explicit OKT_CD_FILE wins over XDG_RUNTIME_DIR,
// which wins over TMPDIR, which falls back to /tmp.
func TestOktCDPathHonorsEnvOverrides(t *testing.T) {
	t.Setenv("OKT_CD_FILE", "/explicit/path/okt-cd")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := oktCDPath(); got != "/explicit/path/okt-cd" {
		t.Fatalf("oktCDPath() = %q, want explicit override", got)
	}

	t.Setenv("OKT_CD_FILE", "")
	if got := oktCDPath(); got != "/run/user/1000/okt-cd" {
		t.Fatalf("oktCDPath() = %q, want XDG_RUNTIME_DIR-derived path", got)
	}

	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "/var/tmp")
	want := "/var/tmp/okt-cd-" + strconv.Itoa(os.Getuid())
	if got := oktCDPath(); got != want {
		t.Fatalf("oktCDPath() = %q, want %q", got, want)
	}
}

// TestWriteOktCDPathRoundTrips ensures the wrapper handshake file lands
// where oktCDPath resolves and contains the project root + trailing newline
// — the format the shell wrapper parses with `head -n 1`.
func TestWriteOktCDPathRoundTrips(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "okt-cd")
	t.Setenv("OKT_CD_FILE", target)

	if err := writeOktCDPath("/work/myproject"); err != nil {
		t.Fatalf("writeOktCDPath() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", target, err)
	}
	got := strings.TrimRight(string(data), "\n")
	if got != "/work/myproject" {
		t.Fatalf("handshake file = %q, want /work/myproject", got)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("handshake file must end with newline; got %q", string(data))
	}
}

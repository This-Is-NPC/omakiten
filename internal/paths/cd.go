package paths

import (
	"os"
	"path/filepath"
	"strconv"
)

// WriteCDPath writes the absolute project root path to the channel the
// shell wrapper reads after the TUI exits. Resolution order, mirroring the
// wrapper itself: $OKT_CD_FILE → $XDG_RUNTIME_DIR/okt-cd → $TMPDIR/okt-cd-$UID
// → /tmp/okt-cd-$UID. Best-effort: an I/O failure here is not surfaced to
// the user because the wrapper treats a missing file as "no cd needed".
func WriteCDPath(root string) error {
	target := CDPath()
	if target == "" {
		return nil
	}
	return os.WriteFile(target, []byte(root+"\n"), 0o600)
}

// CDPath resolves the shell wrapper handshake file.
func CDPath() string {
	if path := os.Getenv("OKT_CD_FILE"); path != "" {
		return path
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "okt-cd")
	}
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	return filepath.Join(tmp, "okt-cd-"+strconv.Itoa(os.Getuid()))
}

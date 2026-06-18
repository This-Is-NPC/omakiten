// Package installscript holds hermetic end-to-end tests for the curl|bash
// bootstrap installer (install.sh). The headline test is the NEGATIVE one
// (S1, council/Hughes): a tampered archive whose sha256 does not match the
// published checksums.txt must abort the installer non-zero and leave NO
// binary on PATH — proving the verify-before-extract gate mirrored from the
// in-app updater (internal/cli/update.go:362-383) actually holds.
//
// The harness is fully self-contained: a local httptest server stands in for
// the GitHub release host (install.sh's GITHUB_API_BASE / GITHUB_DL_BASE are
// pointed at it), the "okt" binary inside the archive is a trivial shell stub,
// and HOME/INSTALL_DIR are temp dirs so nothing touches the host. No network.
package installscript

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeTag = "9.9.9"

// scriptAsset returns the asset filename install.sh will request on the
// current host (e.g. okt_Linux_x86_64.tar.gz), matching its get_os/get_arch.
func scriptAsset(t *testing.T) string {
	t.Helper()
	var osPart string
	switch runtime.GOOS {
	case "linux":
		osPart = "Linux"
	case "darwin":
		osPart = "Darwin"
	default:
		t.Skipf("install.sh only supports Linux/Darwin; current GOOS=%s", runtime.GOOS)
	}
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "arm64"
	default:
		t.Skipf("install.sh only supports amd64/arm64; current GOARCH=%s", runtime.GOARCH)
	}
	return fmt.Sprintf("okt_%s_%s.tar.gz", osPart, arch)
}

// buildArchive returns a .tar.gz containing a single executable "okt" stub
// that answers `--version` and `setup` so the positive path can complete.
func buildArchive(t *testing.T) []byte {
	t.Helper()
	stub := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  --version) echo 'okt 9.9.9 (test stub)' ;;\n" +
		"  setup) echo 'okt setup (test stub) ok' ;;\n" +
		"  *) echo \"okt stub: $*\" ;;\n" +
		"esac\n"

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name: "okt",
		Mode: 0o755,
		Size: int64(len(stub)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write([]byte(stub)); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// installServer fakes the GitHub release host. The checksums.txt it serves
// always lists `recordedSum` for the asset; pass the real sum for the
// positive path or a bogus sum for the tamper case.
func installServer(t *testing.T, asset string, archive []byte, recordedSum string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/This-Is-NPC/omakiten/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v%s"}`, fakeTag)
	})
	dlPath := fmt.Sprintf("/This-Is-NPC/omakiten/releases/download/v%s/", fakeTag)
	mux.HandleFunc(dlPath+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(dlPath+"checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", recordedSum, asset)
	})
	return httptest.NewServer(mux)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// internal/installscript -> repo root is two dirs up.
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// runInstall executes install.sh against the fake server and returns the
// combined output, the install dir, and the command error (nil on exit 0).
func runInstall(t *testing.T, srv *httptest.Server) (string, string, error) {
	t.Helper()
	root := repoRoot(t)
	script := filepath.Join(root, "install.sh")
	home := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"INSTALL_DIR="+installDir,
		"GITHUB_API_BASE="+srv.URL,
		"GITHUB_DL_BASE="+srv.URL,
		"SHELL=/bin/bash",
		"PATH="+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	return string(out), installDir, err
}

// TestInstallSh_TamperedArchive_AbortsNonZeroNoBinary is THE test: a tampered
// archive (sha256 != checksums.txt entry) must abort non-zero and install no
// binary.
func TestInstallSh_TamperedArchive_AbortsNonZeroNoBinary(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	asset := scriptAsset(t)
	archive := buildArchive(t)
	// Record a checksum that does NOT match the served archive => tamper.
	bogus := strings.Repeat("0", 64)

	srv := installServer(t, asset, archive, bogus)
	defer srv.Close()

	out, installDir, err := runInstall(t, srv)

	if err == nil {
		t.Fatalf("expected non-zero exit on checksum mismatch, got success.\noutput:\n%s", out)
	}
	if !strings.Contains(out, "checksum mismatch") {
		t.Errorf("expected a clear 'checksum mismatch' error, got:\n%s", out)
	}
	// No binary may exist on PATH (INSTALL_DIR/okt must be absent).
	if _, statErr := os.Stat(filepath.Join(installDir, "okt")); !os.IsNotExist(statErr) {
		t.Errorf("binary must NOT be installed on mismatch; stat err = %v", statErr)
	}
	t.Logf("tamper-abort OK: exit=%v\n%s", err, out)
}

// TestInstallSh_ValidArchive_Installs covers the positive path: a matching
// checksum lets the installer extract, install, and run the stub.
func TestInstallSh_ValidArchive_Installs(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	asset := scriptAsset(t)
	archive := buildArchive(t)
	sum := fmt.Sprintf("%x", sha256.Sum256(archive))

	srv := installServer(t, asset, archive, sum)
	defer srv.Close()

	out, installDir, err := runInstall(t, srv)
	if err != nil {
		t.Fatalf("expected success on valid checksum, got error %v.\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Checksum OK") {
		t.Errorf("expected 'Checksum OK' confirmation, got:\n%s", out)
	}
	bin := filepath.Join(installDir, "okt")
	if _, statErr := os.Stat(bin); statErr != nil {
		t.Errorf("binary must be installed on valid checksum: %v", statErr)
	}
	t.Logf("positive install OK:\n%s", out)
}

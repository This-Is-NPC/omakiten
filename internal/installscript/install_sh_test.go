// Package installscript holds hermetic end-to-end tests for the curl|bash
// bootstrap installer (install.sh). The headline test is the NEGATIVE one
// (S1, council/Hughes): a tampered archive whose sha256 does not match the
// published checksums.txt must abort the installer non-zero and leave NO
// binary on PATH — proving the verify-before-extract gate mirrored from the
// in-app updater (internal/cli/update.go:362-383) actually holds.
//
// The harness is fully self-contained: a local httptest server stands in for
// the GitHub release host, the "okt" binary inside the archive is a trivial
// shell stub, and HOME/INSTALL_DIR are temp dirs so nothing touches the host.
// Tests that need mirrored checksums opt in explicitly with
// OKT_ALLOW_MIRROR_CHECKSUM + OKT_CHECKSUM_BASE; the default trust-root test
// proves GITHUB_DL_BASE alone cannot redirect checksums.
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
	"sync/atomic"
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

// runInstall executes install.sh against the fake server with the explicit
// mirrored-checksum opt-in used by the positive and tamper harness paths.
func runInstall(t *testing.T, srv *httptest.Server) (string, string, error) {
	return runInstallEnv(t, srv, []string{
		"OKT_ALLOW_MIRROR_CHECKSUM=1",
		"OKT_CHECKSUM_BASE=" + srv.URL,
	}, "")
}

// runInstallEnv executes install.sh against the fake server and returns the
// combined output, the install dir, and the command error (nil on exit 0).
func runInstallEnv(t *testing.T, srv *httptest.Server, extraEnv []string, pathPrefix string) (string, string, error) {
	t.Helper()
	root := repoRoot(t)
	script := filepath.Join(root, "install.sh")
	home := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")
	pathValue := os.Getenv("PATH")
	if pathPrefix != "" {
		pathValue = pathPrefix + string(os.PathListSeparator) + pathValue
	}

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"INSTALL_DIR="+installDir,
		"GITHUB_API_BASE="+srv.URL,
		"GITHUB_DL_BASE="+srv.URL,
		"SHELL=/bin/bash",
		"PATH="+pathValue,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), installDir, err
}

func curlBlocker(t *testing.T) (string, []string, string) {
	t.Helper()
	realCurl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not available")
	}
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "curl.log")
	script := fmt.Sprintf(`#!/bin/sh
url=""
for arg in "$@"; do
  case "$arg" in
    http://*|https://*) url="$arg" ;;
  esac
done
printf '%%s\n' "$url" >> "$CURL_LOG"
case "$url" in
  https://github.com/This-Is-NPC/omakiten/releases/download/v%s/checksums.txt)
    echo "blocked pinned checksum fetch for hermetic test: $url" >&2
    exit 22
    ;;
esac
exec "$REAL_CURL" "$@"
`, fakeTag)
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
		t.Fatalf("write curl blocker: %v", err)
	}
	return dir, []string{"REAL_CURL=" + realCurl, "CURL_LOG=" + logPath}, logPath
}

func TestInstallSh_DownloadMirrorCannotRedirectChecksumByDefault(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	asset := scriptAsset(t)
	archive := buildArchive(t)
	sum := fmt.Sprintf("%x", sha256.Sum256(archive))
	var checksumHits atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/This-Is-NPC/omakiten/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v%s"}`, fakeTag)
	})
	dlPath := fmt.Sprintf("/This-Is-NPC/omakiten/releases/download/v%s/", fakeTag)
	mux.HandleFunc(dlPath+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(dlPath+"checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		checksumHits.Add(1)
		fmt.Fprintf(w, "%s  %s\n", sum, asset)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pathPrefix, extraEnv, _ := curlBlocker(t)
	out, installDir, err := runInstallEnv(t, srv, extraEnv, pathPrefix)

	if err == nil {
		t.Fatalf("expected non-zero exit when pinned checksum fetch is unavailable, got success.\noutput:\n%s", out)
	}
	pinnedURL := fmt.Sprintf("https://github.com/This-Is-NPC/omakiten/releases/download/v%s/checksums.txt", fakeTag)
	if !strings.Contains(out, "Verifying checksum against "+pinnedURL) {
		t.Fatalf("expected checksum verification to use pinned GitHub URL %s, got:\n%s", pinnedURL, out)
	}
	mirrorURL := fmt.Sprintf("%s/This-Is-NPC/omakiten/releases/download/v%s/checksums.txt", srv.URL, fakeTag)
	if strings.Contains(out, mirrorURL) {
		t.Fatalf("checksum URL used artifact mirror by default; output:\n%s", out)
	}
	if hits := checksumHits.Load(); hits != 0 {
		t.Fatalf("artifact mirror served checksums.txt %d time(s); expected 0", hits)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "okt")); !os.IsNotExist(statErr) {
		t.Errorf("binary must NOT be installed when pinned checksum fetch fails; stat err = %v", statErr)
	}
	t.Logf("default trust-root pin OK: exit=%v\n%s", err, out)
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

package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/lifecycle"
)

// tarGzWith builds an in-memory gzipped tar containing entries. The
// production update path reads the same shape goreleaser ships, so
// the test stub must mirror it (binary + auxiliary files) instead of
// returning raw binary bytes that would mask the extract step.
func tarGzWith(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return buf.Bytes()
}

// stubFetcher returns Tag on every call. Err short-circuits the JSON
// path so the unhappy path can be exercised without a real network.
type stubFetcher struct {
	Tag string
	Err error
}

func (s stubFetcher) Latest(context.Context) (string, error) {
	if s.Err != nil {
		return "", s.Err
	}
	return s.Tag, nil
}

// stubDownloader serves per-asset bodies. checksums.txt is auto-
// derived from the assets map when no explicit entry is set, so the
// test author only declares the binary archive + the production
// checksum-verify path stays exercised.
type stubDownloader struct {
	Assets map[string][]byte
	Err    error
}

func (s stubDownloader) Download(_ context.Context, _, asset string) (io.ReadCloser, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	if body, ok := s.Assets[asset]; ok {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	if asset == "checksums.txt" {
		return io.NopCloser(strings.NewReader(autoChecksums(s.Assets))), nil
	}
	return nil, fmt.Errorf("stubDownloader: no body for %s", asset)
}

func autoChecksums(assets map[string][]byte) string {
	var b strings.Builder
	for name, body := range assets {
		sum := sha256.Sum256(body)
		fmt.Fprintf(&b, "%x  %s\n", sum, name)
	}
	return b.String()
}

func TestRunUpdate_CheckUpgradeAvailable(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.20.0"},
		Current: "0.19.0",
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Check: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_available" {
		t.Fatalf("code: got %v want update_available", payload["code"])
	}
	if payload["action"] != "upgrade" {
		t.Fatalf("action: got %v want upgrade", payload["action"])
	}
	if payload["applied"] != false {
		t.Fatalf("applied: got %v want false on --check", payload["applied"])
	}
	if payload["current"] != "0.19.0" || payload["latest"] != "0.20.0" {
		t.Fatalf("current/latest: got %v/%v", payload["current"], payload["latest"])
	}
}

func TestRunUpdate_CheckUpToDate(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Tag: "v0.19.0"},
		Current: "0.19.0",
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Check: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_not_required" {
		t.Fatalf("code: got %v want update_not_required", payload["code"])
	}
	if payload["action"] != "noop" {
		t.Fatalf("action: got %v want noop", payload["action"])
	}
}

func TestRunUpdate_NoopWhenCurrent(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.19.0"},
		Current: "0.19.0",
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_not_required" {
		t.Fatalf("code: got %v want update_not_required", payload["code"])
	}
}

func TestRunUpdate_YesSwapsBinary(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: zip+tar archive shape differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	archive := tarGzWith(t, map[string][]byte{
		"okt":             []byte("NEW_INNER_BINARY"),
		"LICENSE":         []byte("MIT"),
		"README.md":       []byte("readme"),
		"CONTRIBUTING.md": []byte("contrib"),
	})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "update_completed" {
		t.Fatalf("code: got %v want update_completed", payload["code"])
	}
	if payload["applied"] != true {
		t.Fatalf("applied: got %v want true", payload["applied"])
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read bin: %v", err)
	}
	if string(got) != "NEW_INNER_BINARY" {
		t.Fatalf("swap bytes: got %q want NEW_INNER_BINARY (raw archive bytes would indicate extract step is missing)", string(got))
	}
	info, _ := os.Stat(bin)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("swap perms: got %v want 0755", info.Mode().Perm())
	}
}

func TestRunUpdate_ExtractFailureSurfacesCodedError(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix-only path: tar archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Archive missing the `okt` entry: extract should fail before swap.
	archive := tarGzWith(t, map[string][]byte{"LICENSE": []byte("MIT")})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}},
		Current:    "0.19.0",
		BinaryPath: bin,
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected error for archive missing binary entry")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error type: got %T want *domain.CodedError", err)
	}
	if coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("code: got %s want %s", coded.Code, domain.ErrUpdateFailed)
	}

	got, _ := os.ReadFile(bin)
	if string(got) != "OLD" {
		t.Fatalf("binary should remain unchanged on extract failure: got %q", string(got))
	}

	_ = lifecycle.ErrArchiveEntryMissing
}

func TestRunUpdate_FetchFailureSurfacesCodedError(t *testing.T) {
	c := updateClient{
		Fetcher: stubFetcher{Err: errors.New("dial tcp: no route to host")},
		Current: "0.19.0",
	}
	_, err := runUpdate(context.Background(), c, updateInputs{Check: true})
	if err == nil {
		t.Fatalf("runUpdate: expected error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error type: got %T want *domain.CodedError", err)
	}
	if coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("code: got %s want %s", coded.Code, domain.ErrUpdateFailed)
	}
}

func TestRunUpdate_DownloadFailureSurfacesCodedError(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("currentGOOS swap exercises posix path")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Err: errors.New("502 bad gateway")},
		Current:    "0.19.0",
		BinaryPath: bin,
	}
	_, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on download failure: %q", string(got))
	}
}

func TestRunUpdate_ChecksumMismatchAborts(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("PWNED")})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	// checksums.txt lists a hash that does NOT match the archive
	// bytes, simulating CDN tampering.
	checksums := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("DIFFERENT_BINARY")), asset)
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{
			asset:           archive,
			"checksums.txt": []byte(checksums),
		}},
		Current:    "0.19.0",
		BinaryPath: bin,
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected checksum mismatch error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if !strings.Contains(coded.Message, "checksum mismatch") {
		t.Fatalf("message should mention checksum mismatch: %q", coded.Message)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on checksum mismatch: %q", string(got))
	}
}

func TestRunUpdate_ChecksumMissingForAsset(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	archive := tarGzWith(t, map[string][]byte{"okt": []byte("INNER")})
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	// checksums.txt covers a different asset — current platform absent.
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{
			asset:           archive,
			"checksums.txt": []byte("deadbeef  some_other_asset.tar.gz\n"),
		}},
		Current:    "0.19.0",
		BinaryPath: bin,
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected checksum-missing error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
}

func TestRunUpdate_WindowsRefused(t *testing.T) {
	prev := currentGOOS
	currentGOOS = "windows"
	t.Cleanup(func() { currentGOOS = prev })

	c := updateClient{Fetcher: stubFetcher{Tag: "0.20.0"}, Current: "0.19.0"}
	_, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected windows-unsupported error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
}

func TestRunUpdate_DevBuildRefused(t *testing.T) {
	cases := []string{"", "dev", "  "}
	for _, current := range cases {
		c := updateClient{Fetcher: stubFetcher{Tag: "0.20.0"}, Current: current}
		_, err := runUpdate(context.Background(), c, updateInputs{Check: true})
		if err == nil {
			t.Fatalf("dev build %q: expected error", current)
		}
		var coded *domain.CodedError
		if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
			t.Fatalf("dev build %q: error code got %v want ErrValidation", current, err)
		}
	}
}

func TestAssetName_PerPlatform(t *testing.T) {
	cases := []struct {
		os, arch string
		want     string
		err      bool
	}{
		{"linux", "amd64", "okt_Linux_x86_64.tar.gz", false},
		{"linux", "arm64", "okt_Linux_arm64.tar.gz", false},
		{"darwin", "amd64", "okt_Darwin_x86_64.tar.gz", false},
		{"darwin", "arm64", "okt_Darwin_arm64.tar.gz", false},
		{"windows", "amd64", "okt_Windows_x86_64.zip", false},
		{"freebsd", "amd64", "", true},
		{"linux", "riscv64", "", true},
	}
	for _, c := range cases {
		got, err := assetName(c.os, c.arch)
		if c.err {
			if err == nil {
				t.Errorf("assetName(%s,%s): expected error", c.os, c.arch)
			}
			continue
		}
		if err != nil {
			t.Errorf("assetName(%s,%s): %v", c.os, c.arch, err)
			continue
		}
		if got != c.want {
			t.Errorf("assetName(%s,%s): got %q want %q", c.os, c.arch, got, c.want)
		}
	}
}

func TestNormalizeVersion_StripsV(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.19.0", "0.19.0"},
		{"0.19.0", "0.19.0"},
		{"  v1.2.3  ", "1.2.3"},
		{"", ""},
		{"dev", "dev"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestUpdateConfirm_AcceptOnY(t *testing.T) {
	model := updateConfirmModel{current: "0.19.0", latest: "0.20.0"}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	final, ok := updated.(updateConfirmModel)
	if !ok {
		t.Fatalf("model type: got %T want updateConfirmModel", updated)
	}
	if !final.accepted || final.declined {
		t.Fatalf("accepted=%v declined=%v want accepted=true", final.accepted, final.declined)
	}
}

func TestUpdateConfirm_DeclineOnN(t *testing.T) {
	model := updateConfirmModel{current: "0.19.0", latest: "0.20.0"}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	final, ok := updated.(updateConfirmModel)
	if !ok {
		t.Fatalf("model type: got %T want updateConfirmModel", updated)
	}
	if final.accepted || !final.declined {
		t.Fatalf("accepted=%v declined=%v want declined=true", final.accepted, final.declined)
	}
}

// TestRunUpdate_AssetTooLargeAborts pins the download size cap. A
// compromised CDN or MITM that streams a payload above maxAssetSize
// must be rejected with a coded ErrUpdateFailed before the SHA256
// verify step (which would itself buffer the entire body in memory).
// The fake downloader returns maxAssetSize+1 bytes of garbage so the
// LimitReader trips before checksum compare runs.
func TestRunUpdate_AssetTooLargeAborts(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("posix archive shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	oversized := bytes.Repeat([]byte{0x42}, int(maxAssetSize)+1)
	asset, err := assetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	c := updateClient{
		Fetcher: stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Assets: map[string][]byte{
			asset: oversized,
		}},
		Current:    "0.19.0",
		BinaryPath: bin,
	}
	_, err = runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err == nil {
		t.Fatalf("expected size-cap error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUpdateFailed {
		t.Fatalf("error: got %v want ErrUpdateFailed", err)
	}
	if !strings.Contains(coded.Message, "size cap") {
		t.Fatalf("message should mention size cap: %q", coded.Message)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Fatalf("binary mutated on size-cap rejection: %q", string(got))
	}
}

func TestAtomicSwap_OverwritesAndChmods(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	body := strings.NewReader("REPLACED")
	if err := atomicSwap(bin, body); err != nil {
		t.Fatalf("atomicSwap: %v", err)
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "REPLACED" {
		t.Fatalf("atomicSwap bytes: got %q want REPLACED", string(got))
	}
}

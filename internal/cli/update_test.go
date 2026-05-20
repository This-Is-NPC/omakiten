package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

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

// stubDownloader returns Body on Download. Used to assert atomicSwap
// writes the expected bytes onto the target path without hitting
// github.com/releases/download.
type stubDownloader struct {
	Body []byte
	Err  error
}

func (s stubDownloader) Download(context.Context, string, string) (io.ReadCloser, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	return io.NopCloser(strings.NewReader(string(s.Body))), nil
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
	payload := res.(map[string]any)
	if payload["code"] != "update_completed" {
		t.Fatalf("code: got %v want update_completed", payload["code"])
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
	payload := res.(map[string]any)
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
	payload := res.(map[string]any)
	if payload["code"] != "update_not_required" {
		t.Fatalf("code: got %v want update_not_required", payload["code"])
	}
}

func TestRunUpdate_YesSwapsBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	c := updateClient{
		Fetcher:    stubFetcher{Tag: "0.20.0"},
		Downloader: stubDownloader{Body: []byte("NEW_BINARY_BYTES")},
		Current:    "0.19.0",
		BinaryPath: bin,
	}
	res, err := runUpdate(context.Background(), c, updateInputs{Yes: true})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	payload := res.(map[string]any)
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
	if string(got) != "NEW_BINARY_BYTES" {
		t.Fatalf("swap bytes: got %q want NEW_BINARY_BYTES", string(got))
	}
	info, _ := os.Stat(bin)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("swap perms: got %v want 0755", info.Mode().Perm())
	}
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
	final := updated.(updateConfirmModel)
	if !final.accepted || final.declined {
		t.Fatalf("accepted=%v declined=%v want accepted=true", final.accepted, final.declined)
	}
}

func TestUpdateConfirm_DeclineOnN(t *testing.T) {
	model := updateConfirmModel{current: "0.19.0", latest: "0.20.0"}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	final := updated.(updateConfirmModel)
	if final.accepted || !final.declined {
		t.Fatalf("accepted=%v declined=%v want declined=true", final.accepted, final.declined)
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

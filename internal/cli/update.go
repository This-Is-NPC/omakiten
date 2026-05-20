package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"omakiten/internal/domain"
	"omakiten/internal/lifecycle"
)

// updateRepo is the GitHub repository the in-binary updater polls
// for release tags. Constant — tests stub the LatestFetcher /
// AssetDownloader interfaces instead of rewiring the repo string.
const updateRepo = "This-Is-NPC/omakiten"

// currentGOOS is goruntime.GOOS at process start. Kept as a package
// var so update_test.go can swap it (e.g. force "windows") without
// running on a real Windows host.
var currentGOOS = goruntime.GOOS

// LatestFetcher resolves the latest published release tag (e.g.
// "0.19.0", without the "v" prefix). Injected into runUpdate so the
// test suite can return a deterministic value without hitting GitHub.
type LatestFetcher interface {
	Latest(ctx context.Context) (string, error)
}

// AssetDownloader fetches a release-asset tarball/zip and returns its
// body for atomicSwap to consume. Injected so the test suite can
// serve a tmp file instead of hitting github.com/releases/download.
type AssetDownloader interface {
	Download(ctx context.Context, tag, asset string) (io.ReadCloser, error)
}

// updateClient bundles the two injected dependencies plus the
// command-version + binary-path resolution helpers so RunE can stay
// tiny. Production wiring is built by defaultUpdateClient; tests pass
// a struct with stubbed Fetcher / Downloader / BinaryPath fields.
type updateClient struct {
	Fetcher    LatestFetcher
	Downloader AssetDownloader
	// Current is the running binary's version, sourced from the
	// cobra root --version flag (set at build time via
	// `-ldflags -X main.version=...`). Tests pass a literal string.
	Current string
	// BinaryPath is the on-disk path the swap targets. Defaults to
	// os.Executable(); tests override to point at a tmp file so
	// assertions can read the post-swap bytes.
	BinaryPath string
}

func newUpdateCommand(opts *runtimeOptions) *cobra.Command {
	var (
		yes   bool
		check bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: opts.t("cli.update.short"),
		Long:  opts.t("cli.update.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				client, err := defaultUpdateClient(cmd.Root().Version)
				if err != nil {
					return nil, err
				}
				return runUpdate(ctx, client, updateInputs{Check: check, Yes: yes})
			})
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, opts.t("cli.update.flag.yes"))
	cmd.Flags().BoolVar(&check, "check", false, opts.t("cli.update.flag.check"))
	return cmd
}

// updateInputs is the resolved flag set runUpdate consumes. Kept as a
// struct (rather than two booleans threaded through the signature) so
// future flags (--channel beta, --prerelease) drop in without
// breaking call sites.
type updateInputs struct {
	Check bool
	Yes   bool
}

// runUpdate is the headless-friendly entry point: resolve latest,
// compare, optionally confirm + swap. Returns the JSON envelope
// payload. --check short-circuits before any side-effect.
func runUpdate(ctx context.Context, c updateClient, inputs updateInputs) (any, error) {
	if current := strings.TrimSpace(c.Current); current == "" || current == "dev" {
		return nil, domain.NewError(domain.ErrValidation, t("cli.update.err.dev_build"), nil)
	}
	if currentGOOS == "windows" {
		return nil, domain.NewError(domain.ErrUpdateFailed, t("cli.update.err.windows_unsupported"), nil)
	}

	latest, err := c.Fetcher.Latest(ctx)
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.fetch_latest"), err.Error()), nil)
	}
	current := normalizeVersion(c.Current)
	latest = normalizeVersion(latest)

	action := "upgrade"
	if current == latest {
		action = "noop"
	}

	if inputs.Check {
		code := "update_not_required"
		if action == "upgrade" {
			code = "update_available"
		}
		return map[string]any{
			"code":    code,
			"current": current,
			"latest":  latest,
			"action":  action,
			"applied": false,
		}, nil
	}

	if action == "noop" {
		return map[string]any{
			"code":    "update_not_required",
			"current": current,
			"latest":  latest,
			"action":  action,
			"applied": false,
		}, nil
	}

	if !inputs.Yes {
		if !stdinIsTTY() {
			return nil, domain.NewError(domain.ErrValidation, t("cli.update.picker.no_tty"), nil)
		}
		confirmed, err := runUpdateConfirm(ctx, current, latest)
		if err != nil {
			return nil, err
		}
		if !confirmed {
			return nil, domain.NewError(domain.ErrValidation, t("cli.update.picker.aborted"), nil)
		}
	}

	asset, err := assetName(currentGOOS, goruntime.GOARCH)
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, err.Error(), nil)
	}

	expectedSum, err := fetchAssetChecksum(ctx, c.Downloader, latest, asset)
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.fetch_checksum"), err.Error()), nil)
	}

	body, err := c.Downloader.Download(ctx, latest, asset)
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.download_asset"), asset, err.Error()), nil)
	}
	defer body.Close()

	archiveBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.download_asset"), asset, err.Error()), nil)
	}
	gotSum := fmt.Sprintf("%x", sha256.Sum256(archiveBytes))
	if !strings.EqualFold(gotSum, expectedSum) {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.checksum_mismatch"), expectedSum, gotSum), nil)
	}

	binary, err := lifecycle.ExtractBinary(bytes.NewReader(archiveBytes), currentGOOS, lifecycle.BinaryName())
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.extract_asset"), lifecycle.BinaryName(), err.Error()), nil)
	}

	if err := atomicSwap(c.BinaryPath, bytes.NewReader(binary)); err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.swap_binary"), c.BinaryPath, err.Error()), nil)
	}

	return map[string]any{
		"code":        "update_completed",
		"current":     current,
		"latest":      latest,
		"action":      action,
		"applied":     true,
		"binary_path": c.BinaryPath,
	}, nil
}

// updateConfirmModel is the bubbletea program backing the y/n
// confirmation prompt. Tiny by design — the only state is the
// current/latest pair and the decision flags — so the test suite can
// drive it with two key messages instead of constructing a full
// picker.
type updateConfirmModel struct {
	current  string
	latest   string
	accepted bool
	declined bool
}

func (m updateConfirmModel) Init() tea.Cmd { return nil }

func (m updateConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "y", "Y", "enter":
		m.accepted = true
		return m, tea.Quit
	case "n", "N", "ctrl+c", "esc":
		m.declined = true
		return m, tea.Quit
	}
	return m, nil
}

func (m updateConfirmModel) View() string {
	return "\n" + t("cli.update.picker.title") + "\n\n  " +
		fmt.Sprintf(t("cli.update.picker.line"), m.current, m.latest) + "\n\n  " +
		t("cli.update.picker.hint") + "\n"
}

func runUpdateConfirm(ctx context.Context, current, latest string) (bool, error) {
	model := updateConfirmModel{current: current, latest: latest}
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))
	final, err := prog.Run()
	if err != nil {
		return false, fmt.Errorf("run update picker: %w", err)
	}
	result, ok := final.(updateConfirmModel)
	if !ok {
		return false, fmt.Errorf("update picker returned unexpected model type %T", final)
	}
	return result.accepted && !result.declined, nil
}

// defaultUpdateClient builds the production wiring: HTTP-backed
// LatestFetcher + AssetDownloader pointing at the GitHub releases API,
// the os.Executable() binary path, and the cobra Version literal.
//
// BinaryPath here intentionally tracks the *running* binary rather
// than lifecycle.BinaryPath(home) (the canonical install location):
// a user who copied the binary to /tmp/okt-test and runs the update
// from there expects /tmp/okt-test to be the file that gets swapped.
// The uninstall command takes the opposite stance — see uninstall.go
// for why it targets the canonical install dir instead.
func defaultUpdateClient(version string) (updateClient, error) {
	bin, err := os.Executable()
	if err != nil {
		return updateClient{}, err
	}
	resolved, err := filepath.EvalSymlinks(bin)
	if err == nil {
		bin = resolved
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	return updateClient{
		Fetcher:    &githubLatestFetcher{Repo: updateRepo, HTTP: hc},
		Downloader: &githubAssetDownloader{Repo: updateRepo, HTTP: hc},
		Current:    version,
		BinaryPath: bin,
	}, nil
}

// githubLatestFetcher polls
// `https://api.github.com/repos/<repo>/releases/latest` and parses the
// tag_name field. The bash installer uses the same endpoint so the
// two surfaces converge on the same release.
//
// GitHub's /releases/latest endpoint excludes drafts and prereleases
// by design, so `--check` will not flap on every RC tag. If the
// repository starts publishing prereleases through this endpoint we
// must switch to GET /releases?per_page=10 + filter `prerelease`.
type githubLatestFetcher struct {
	Repo string
	HTTP *http.Client
}

func (g *githubLatestFetcher) Latest(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", g.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api status %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v"), nil
}

// githubAssetDownloader streams the platform-matched asset from the
// release-download URL. The body is returned untouched so atomicSwap
// can consume the tarball directly; the caller is responsible for
// closing it.
type githubAssetDownloader struct {
	Repo string
	HTTP *http.Client
}

func (g *githubAssetDownloader) Download(ctx context.Context, tag, asset string) (io.ReadCloser, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", g.Repo, tag, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// assetName maps GOOS/GOARCH to the asset filename install.sh
// constructs: `okt_<os>_<arch>.tar.gz`. The bash side capitalizes the
// OS token (`Linux`, `Darwin`) and renames the architecture so the
// goreleaser-published asset matches.
func assetName(goos, goarch string) (string, error) {
	osTok := ""
	switch goos {
	case "linux":
		osTok = "Linux"
	case "darwin":
		osTok = "Darwin"
	case "windows":
		osTok = "Windows"
	default:
		return "", fmt.Errorf(t("cli.update.err.unsupported_platform"), goos, goarch)
	}
	archTok := ""
	switch goarch {
	case "amd64":
		archTok = "x86_64"
	case "arm64":
		archTok = "arm64"
	default:
		return "", fmt.Errorf(t("cli.update.err.unsupported_platform"), goos, goarch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("okt_%s_%s%s", osTok, archTok, ext), nil
}

// fetchAssetChecksum downloads `checksums.txt` from the release and
// returns the hex sha256 expected for `asset`. goreleaser publishes
// the file as `<sha256>  <filename>` lines (two-space separator) —
// the same shape `sha256sum -c` consumes.
func fetchAssetChecksum(ctx context.Context, dl AssetDownloader, tag, asset string) (string, error) {
	body, err := dl.Download(ctx, tag, "checksums.txt")
	if err != nil {
		return "", err
	}
	defer body.Close()
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[1] == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksum for %s not in checksums.txt", asset)
}

// atomicSwap writes body to the binary path via a sibling temp file
// then renames it over the original. Same-filesystem rename is atomic
// on POSIX; Windows callers are refused upstream in runUpdate so the
// EXE-in-use shape doesn't surface here.
func atomicSwap(path string, body io.Reader) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".okt-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	swapped := false
	defer func() {
		if !swapped {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	swapped = true
	return nil
}

// normalizeVersion strips a leading "v" so the github API tag
// ("v0.19.0") and the build-injected --version ("0.19.0") compare
// equal. Empty strings flow through untouched so "dev" / "" stays as
// the caller wrote it.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimPrefix(v, "v")
}

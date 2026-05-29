package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"omakiten/internal/domain"
	"omakiten/internal/lifecycle"
	"omakiten/internal/sqlite"
)

// updateBackupForOpts constructs the pre-swap BackupService through
// the shared buildCLIBackupService helper in strict mode — the
// auto-backup is non-optional per #191 AC #36 / #39 so any failure to
// resolve the backup dir or load the bundle aborts the update before
// the swap. Callers must propagate the error to the JSON envelope so
// the user sees the underlying cause; silent bypass to `client.Backup
// = nil` (the pre-fix shape) would let the destructive flow run
// without its safety net.
func updateBackupForOpts(cmd *cobra.Command, opts *runtimeOptions) (updateBackupRunner, error) {
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return nil, err
	}
	svc, _, err := buildCLIBackupService(cmd, opts, dbPath, true)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

// updateRepo is the GitHub repository the in-binary updater polls
// for release tags. Constant — tests stub the LatestFetcher /
// AssetDownloader interfaces instead of rewiring the repo string.
const updateRepo = "This-Is-NPC/omakiten"

// maxAssetSize caps the per-asset download body so a compromised CDN
// or MITM cannot OOM the host by streaming an arbitrarily large
// payload before the SHA256 verify step runs. The published release
// archives sit comfortably below 50 MiB; the 256 MiB ceiling leaves
// generous headroom for future bundled assets while still rejecting
// pathological payloads.
const maxAssetSize int64 = 256 << 20

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

// updateValidatorResult is the parsed output of a single staged-binary
// health check. The fields mirror the structured payload `okt config
// validate --migrate` emits under details (#365 AC 1): OK gates the
// swap, Errors carries the per-kind {kind, path, message,
// suggested_command} entries the user surfaces to repair the bundle,
// RawOutput preserves the validator's stdout so the update envelope
// can echo it back for triage.
type updateValidatorResult struct {
	OK        bool
	Errors    []map[string]any
	RawOutput []byte
}

// updateValidatorFn runs the staged binary at binaryPath against the
// caller's configPath and reports whether the bundle still validates
// under the new schema. A non-nil error is reserved for infrastructure
// failures (exec could not spawn, output unreadable); a zero-Error
// validator non-zero exit returns OK=false with parsed details.
type updateValidatorFn func(ctx context.Context, binaryPath, configPath string) (updateValidatorResult, error)

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
	// Backup is a direct backup runner injection. Production wires
	// BackupFactory instead so resolution happens lazily past the
	// --check / noop short-circuits. Kept for direct tests that
	// exercise the swap path without going through a factory.
	Backup updateBackupRunner
	// BackupFactory resolves the pre-swap backup runner only when
	// the swap path is actually about to fire. Wired by RunE so an
	// unresolvable BackupDir (or unloadable bundle) does not abort
	// `okt update --check` and `okt update` on a current binary
	// (noop) — both paths skip the binary swap and therefore do not
	// need a recovery snapshot. nil falls back to Backup.
	BackupFactory func(ctx context.Context) (updateBackupRunner, error)
	// ConfigPath is the active omakiten.yaml the staged-binary
	// validator runs against (#365 AC 2). Empty disables the
	// pre-swap health check — tests use that path to exercise the
	// post-validate flow without exec'ing a real subprocess.
	ConfigPath string
	// Validator gates the swap on a successful `okt config validate
	// --migrate` run against the *new* binary so schema drift caught
	// only by the upcoming release surfaces here, pre-swap, instead
	// of as a silent next-launch failure (#365 AC 2). nil = swap
	// proceeds unchanged (back-compat for direct tests that already
	// vet the swap path without a validator).
	Validator updateValidatorFn
	// EventStore is the activity-emit sink the runUpdate flow writes
	// healthcheck.passed / healthcheck.failed / swap.completed /
	// swap.aborted rows through (#369 AC 1). nil disables emission;
	// activity-write failures are swallowed regardless so the swap's
	// success criterion is the binary state, not the audit row.
	EventStore healthCheckEventStore
}

// updateBackupRunner is the narrow port runUpdate uses to invoke the
// pre-swap snapshot. Local alias for app.BackupRunner so this file
// does not depend on the app package directly (the cli already
// imports app elsewhere, but the narrow alias keeps the test wiring
// terse).
type updateBackupRunner interface {
	Run(ctx context.Context) (string, error)
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
				client.BackupFactory = func(_ context.Context) (updateBackupRunner, error) {
					return updateBackupForOpts(cmd, opts)
				}
				// ConfigPath resolution intentionally lives at RunE
				// (not defaultUpdateClient) so the production wiring
				// honours --config / repo-local discovery the same
				// way every other CLI surface does. Resolution
				// failure here is non-fatal: empty ConfigPath drops
				// the validator branch and the update behaves
				// exactly as it did pre-#365 — the user still gets
				// the binary swap, just without the pre-flight
				// health check. The fallback is logged to stderr so
				// the user sees the degraded mode instead of a
				// silent skip.
				if cfgPath, cfgErr := opts.resolvedConfigPath(); cfgErr == nil {
					client.ConfigPath = cfgPath
				} else {
					fmt.Fprintf(os.Stderr, "okt update: config path unavailable, skipping pre-swap health check: %v\n", cfgErr)
				}
				client.Validator = defaultUpdateValidator
				// EventStore opens the SQLite store so runUpdate can
				// append healthcheck.* / swap.* rows to the activity
				// log (#369). Resolution failure (state-dir
				// unwritable etc.) is non-fatal — emission becomes a
				// no-op and the swap path remains intact. Both
				// failure modes get a stderr breadcrumb so the
				// missing audit row is traceable.
				if dbPath, dbErr := opts.resolvedDBPath(); dbErr == nil {
					if store, openErr := sqlite.Open(ctx, dbPath); openErr == nil {
						defer store.Close()
						client.EventStore = store
					} else {
						fmt.Fprintf(os.Stderr, "okt update: sqlite open %s failed, activity rows skipped: %v\n", dbPath, openErr)
					}
				} else {
					fmt.Fprintf(os.Stderr, "okt update: db path unavailable, activity rows skipped: %v\n", dbErr)
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

	archiveBytes, err := io.ReadAll(io.LimitReader(body, maxAssetSize+1))
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.download_asset"), asset, err.Error()), nil)
	}
	if int64(len(archiveBytes)) > maxAssetSize {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.asset_too_large"), asset, maxAssetSize), nil)
	}
	gotSum := fmt.Sprintf("%x", sha256.Sum256(archiveBytes))
	if !strings.EqualFold(gotSum, expectedSum) {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.checksum_mismatch"), expectedSum, gotSum), nil)
	}

	binary, err := lifecycle.ExtractBinary(bytes.NewReader(archiveBytes), currentGOOS, lifecycle.BinaryName())
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.extract_asset"), lifecycle.BinaryName(), err.Error()), nil)
	}

	// Stage the new binary next to the live path so the rename at the
	// end of this function is an atomic same-filesystem move. The
	// staged file is the artefact the validator execs — running the
	// new binary's `config validate --migrate` against the on-disk
	// config catches schema drift introduced by this release before
	// the swap, satisfying the pre-swap gate from #365 AC 2.
	stagedPath, err := stageBinary(c.BinaryPath, bytes.NewReader(binary))
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.stage_binary_fmt"), err.Error()), nil)
	}
	swapped := false
	defer func() {
		if !swapped {
			_ = os.Remove(stagedPath)
		}
	}()

	if c.Validator != nil && strings.TrimSpace(c.ConfigPath) != "" {
		result, vErr := c.Validator(ctx, stagedPath, c.ConfigPath)
		if vErr != nil {
			return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.config_validation_exec_fmt"), vErr.Error()), map[string]any{
				"reason":      "config_validation_exec_failed",
				"current":     current,
				"latest":      latest,
				"binary_path": c.BinaryPath,
				"staged_path": stagedPath,
				"cause":       vErr.Error(),
			})
		}
		if !result.OK {
			firstKind := ""
			if len(result.Errors) > 0 {
				if k, ok := result.Errors[0]["kind"].(string); ok {
					firstKind = k
				}
			}
			emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateHealthCheckFailed, map[string]any{
				"from_version":               current,
				"to_version":                 latest,
				"staged_path":                stagedPath,
				"validator_error_count":      len(result.Errors),
				"validator_first_error_kind": firstKind,
				"validator_raw_excerpt":      string(result.RawOutput),
			})
			emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateSwapAborted, map[string]any{
				"from_version":          current,
				"to_version":            latest,
				"reason":                "config_validation_failed",
				"validator_error_count": len(result.Errors),
			})
			return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.config_validation_failed_fmt"), len(result.Errors), firstKind), map[string]any{
				"reason":        "config_validation_failed",
				"current":       current,
				"latest":        latest,
				"binary_path":   c.BinaryPath,
				"staged_path":   stagedPath,
				"errors":        result.Errors,
				"validator_raw": string(result.RawOutput),
			})
		}
		emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateHealthCheckPassed, map[string]any{
			"from_version": current,
			"to_version":   latest,
			"binary_path":  c.BinaryPath,
			"staged_path":  stagedPath,
		})
	}

	// Pre-swap snapshot: write a recovery .db under StateDir/backups
	// AFTER the validator gate so failed health checks do not leave
	// orphan backups (#365 AC 3). BackupFactory still resolves lazily
	// so `okt update --check` and the noop fast path skip it
	// entirely.
	backupRunner := c.Backup
	if c.BackupFactory != nil {
		runner, factoryErr := c.BackupFactory(ctx)
		if factoryErr != nil {
			return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.backup_failed_fmt"), factoryErr.Error()), map[string]any{
				"reason": "backup_failed",
				"cause":  factoryErr.Error(),
			})
		}
		backupRunner = runner
	}
	var backupPath string
	if backupRunner != nil {
		path, err := backupRunner.Run(ctx)
		if err != nil {
			return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.backup_failed_fmt"), err.Error()), map[string]any{
				"reason": "backup_failed",
				"cause":  err.Error(),
			})
		}
		backupPath = path
	}

	if err := swapStagedBinary(stagedPath, c.BinaryPath); err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.swap_binary"), c.BinaryPath, err.Error()), nil)
	}
	swapped = true

	emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateSwapCompleted, map[string]any{
		"from_version": current,
		"to_version":   latest,
		"binary_path":  c.BinaryPath,
		"backup_path":  backupPath,
	})

	return map[string]any{
		"code":        "update_completed",
		"current":     current,
		"latest":      latest,
		"action":      action,
		"applied":     true,
		"binary_path": c.BinaryPath,
		"backup_path": backupPath,
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

// defaultUpdateValidator runs the staged binary's `okt config validate
// --migrate --config <configPath>` and parses its JSON envelope. A
// non-zero exit code from the validator is the normal failure path
// (returned as result.OK=false, not as a Go error) — only spawn
// failures or unparseable output produce an error here. The validator
// inherits the parent's env so the staged binary sees the same
// `OMAKITEN_HOME` / XDG state the live binary would on next launch.
//
// Stdout and stderr are captured into SEPARATE buffers — the staged
// binary's `emitBundleWarnings` writes to stderr while the JSON
// envelope lands on stdout, and a shared buffer would prepend the
// warning text to the JSON and break `json.Unmarshal` with an
// `invalid character` error. Stderr is mirrored to the parent's
// stderr after the call so warnings stay visible without poisoning
// the parse.
func defaultUpdateValidator(ctx context.Context, binaryPath, configPath string) (updateValidatorResult, error) {
	cmd := exec.CommandContext(ctx, binaryPath, "config", "validate", "--migrate", "--config", configPath)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	// Mirror the validator subprocess's stderr to the parent, capped
	// so a misbehaving validator that floods stderr with multi-MB
	// output cannot block the parse or hide the JSON envelope behind
	// noise. The first 64 KiB carries the warnings that motivated
	// the split; anything larger is a runaway and warrants the
	// truncation marker.
	if stderr.Len() > 0 {
		const stderrMirrorCap = 64 << 10
		buf := stderr.Bytes()
		if len(buf) > stderrMirrorCap {
			_, _ = os.Stderr.Write(buf[:stderrMirrorCap])
			fmt.Fprintf(os.Stderr, "\n[okt update: validator stderr truncated after %d bytes; %d more bytes suppressed]\n", stderrMirrorCap, len(buf)-stderrMirrorCap)
		} else {
			_, _ = os.Stderr.Write(buf)
		}
	}

	var exitErr *exec.ExitError
	exitedNonZero := errors.As(runErr, &exitErr)
	if exitedNonZero {
		// Non-zero exit is the documented validator-fail path;
		// surface OK=false with parsed errors rather than treating
		// it as exec infrastructure breakage.
		runErr = nil
	}
	if runErr != nil {
		return updateValidatorResult{OK: false, RawOutput: stdout.Bytes()}, runErr
	}

	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 {
		// Non-zero exit + empty stdout is still a structured failure
		// from the user's perspective (the validator decided to
		// abort); only the zero-exit + empty stdout combination is
		// genuine infra weirdness worth surfacing as an error.
		if exitedNonZero {
			return updateValidatorResult{OK: false, RawOutput: nil}, nil
		}
		return updateValidatorResult{OK: false, RawOutput: nil}, fmt.Errorf("validator produced no output")
	}

	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		return updateValidatorResult{OK: false, RawOutput: raw}, fmt.Errorf("parse validator envelope: %w", err)
	}

	result := updateValidatorResult{RawOutput: raw}
	if okFlag, _ := env["ok"].(bool); okFlag {
		result.OK = true
		return result, nil
	}

	// Failure envelope: details.errors carries the structured kinds.
	if details, ok := env["details"].(map[string]any); ok {
		if errs, ok := details["errors"].([]any); ok {
			for _, e := range errs {
				if m, ok := e.(map[string]any); ok {
					result.Errors = append(result.Errors, m)
				}
			}
		}
	}
	return result, nil
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

// stageBinary writes body to a sibling tmp file next to dst with +x
// perms and returns its path. The two-step "stage then swap" split
// (#365 AC 2) lets runUpdate run the validator against the staged
// file before any atomic move clobbers the running binary: a failed
// health check removes the tmp and leaves the install untouched.
// Same-filesystem placement keeps the eventual rename atomic.
func stageBinary(dst string, body io.Reader) (string, error) {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".okt-update-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// swapStagedBinary atomically renames a staged file over dst. POSIX
// same-filesystem rename is atomic; Windows callers are refused
// upstream in runUpdate so the EXE-in-use shape doesn't surface here.
func swapStagedBinary(stagedPath, dst string) error {
	return os.Rename(stagedPath, dst)
}

// atomicSwap writes body to the binary path via a sibling temp file
// then renames it over the original. Kept as a thin wrapper over
// stageBinary + swapStagedBinary so the existing direct callers
// (`internal/cli/update_test.go`) still compile while runUpdate uses
// the split pair to gate the rename on the validator.
func atomicSwap(path string, body io.Reader) error {
	staged, err := stageBinary(path, body)
	if err != nil {
		return err
	}
	if err := swapStagedBinary(staged, path); err != nil {
		_ = os.Remove(staged)
		return err
	}
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

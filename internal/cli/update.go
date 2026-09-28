package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	"omakiten/internal/paths"
	"omakiten/internal/releaseverify"
	"omakiten/internal/sqlite"
	"omakiten/internal/updater"
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
// for release tags. Constant — tests stub the updater.LatestFetcher /
// updater.AssetDownloader interfaces instead of rewiring the repo string.
const updateRepo = "This-Is-NPC/omakiten"

// currentGOOS is goruntime.GOOS at process start. Kept as a package
// var so update_test.go can swap it (e.g. force "windows") without
// running on a real Windows host.
var currentGOOS = goruntime.GOOS

// updateValidatorResult is the parsed output of a single staged-binary
// health check. The fields mirror the structured payload `okt config
// validate` emits under details: OK gates the
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

type updateDefaultsRefresherFn func(ctx context.Context, binaryPath string) error

type updateEventStoreFactory func(ctx context.Context) (healthCheckEventStore, func())

// updateClient bundles the two injected dependencies plus the
// command-version + binary-path resolution helpers so RunE can stay
// tiny. Production wiring is built by defaultUpdateClient; tests pass
// a struct with stubbed Fetcher / Downloader / BinaryPath fields.
type updateClient struct {
	Fetcher    updater.LatestFetcher
	Downloader updater.AssetDownloader
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
	// Validator gates the swap on a successful `okt config validate`
	// run against the *new* binary so schema drift caught
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
	// EventStoreFactory lazily opens the activity store after version checks,
	// confirmation, and staging. Its cleanup is deferred across validation,
	// backup, swap, and finish; failures are telemetry-only.
	EventStoreFactory updateEventStoreFactory
	// DefaultsRefresher runs the newly swapped binary's direct defaults
	// refresh command after the binary update is durable. nil skips the
	// post-swap refresh, which keeps direct unit tests from exec'ing fake
	// binary bytes; production wiring always supplies it.
	DefaultsRefresher updateDefaultsRefresherFn
	// DefaultsRefreshConfigPath is the exact resolved profile path passed
	// to the post-swap refresh subprocess. It also drives the repair
	// command surfaced when the binary swap succeeded but refresh failed.
	DefaultsRefreshConfigPath string
	// ReleaseVerifier authenticates the signed release metadata before the
	// downloaded archive is trusted. nil aborts the update: there is no
	// unsigned path, so an unset verifier is a bug, not a fallback.
	ReleaseVerifier releaseVerifierFn
}

// releaseVerifierFn authenticates one downloaded release set and returns the
// digest the archive must have. Injected so the updater tests can drive the
// swap path without minting Sigstore bundles, while the strict policy itself
// is exercised in internal/releaseverify.
type releaseVerifierFn func(ctx context.Context, rel releaseverify.Release) (releaseverify.Result, error)

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
		yes          bool
		check        bool
		skipDefaults bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: opts.t("cli.update.short"),
		Long:  opts.t("cli.update.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runUpdateCommand(ctx, cmd, opts, updateInputs{Check: check, Yes: yes, SkipDefaults: skipDefaults})
			})
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, opts.t("cli.update.flag.yes"))
	cmd.Flags().BoolVar(&check, "check", false, opts.t("cli.update.flag.check"))
	cmd.Flags().BoolVar(&skipDefaults, "skip-defaults", false, opts.t("cli.update.flag.skip_defaults"))
	return cmd
}

func runUpdateCommand(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, inputs updateInputs) (any, error) {
	client, err := defaultUpdateClientFactory(cmd.Root().Version)
	if err != nil {
		return nil, err
	}
	client.BackupFactory = func(_ context.Context) (updateBackupRunner, error) {
		return updateBackupForOpts(cmd, opts)
	}
	if inputs.Check {
		return runUpdate(ctx, client, inputs)
	}
	if err := prepareUpdateDiscovery(ctx, opts); err != nil {
		return nil, err
	}
	if !inputs.SkipDefaults {
		refreshConfigPath, err := resolvedUpdateConfigPathForRefresh(opts)
		if err != nil {
			return nil, err
		}
		client.DefaultsRefreshConfigPath = refreshConfigPath
		client.DefaultsRefresher = updateDefaultsRefresherForConfigPath(refreshConfigPath)
	}
	if err := wireHealthCheckEmission(opts, &client); err != nil {
		return nil, err
	}
	return runUpdate(ctx, client, inputs)
}

func prepareUpdateDiscovery(ctx context.Context, opts *runtimeOptions) error {
	if opts == nil || opts.configPath != "" || (opts.project == "" && opts.projectID == 0) {
		return nil
	}
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return err
	}
	store, err := sqlite.OpenCurrentReadOnly(ctx, dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	start, err := opts.resolveDiscoveryStart(ctx, store)
	if err != nil {
		return err
	}
	opts.discoveryStart = start
	return nil
}

func resolvedUpdateConfigPathForRefresh(opts *runtimeOptions) (string, error) {
	if opts == nil {
		return "", nil
	}
	configPath, err := opts.resolvedConfigPath()
	if err != nil {
		if opts.configPath != "" || opts.project != "" || opts.projectID != 0 {
			return "", err
		}
		return "", nil
	}
	return configPath, nil
}

// wireHealthCheckEmission resolves the runtime knobs the production
// `runUpdate` needs to fire the pre-swap health check (#365) and the
// matching activity rows (#369), mutating client in place. Config path
// resolution is fail-closed because skipping staged validation could swap an
// unsafe binary. DB resolution remains fail-soft because activity emission is
// observability, not an update safety gate.
//
// Extracted as a Sprout Method (Feathers) so newUpdateCommand's
// RunE closure stays at one level of abstraction; the per-finding
// stderr fprints stay co-located with the resolution they
// guard against.
func wireHealthCheckEmission(opts *runtimeOptions, client *updateClient) error {
	if opts == nil {
		return domain.NewError(domain.ErrValidation, "update config path is unavailable; refusing to update", nil)
	}
	if cfgPath, cfgErr := opts.resolvedConfigPath(); cfgErr == nil {
		client.ConfigPath = cfgPath
	} else {
		return domain.NewError(domain.ErrValidation, "update config path is unavailable; refusing to update: "+cfgErr.Error(), nil)
	}
	client.Validator = defaultUpdateValidator

	dbPath, dbErr := opts.resolvedDBPath()
	if dbErr != nil {
		fmt.Fprintf(os.Stderr, "okt update: db path unavailable, activity rows skipped: %v\n", dbErr)
		return nil
	}
	client.EventStoreFactory = func(openCtx context.Context) (healthCheckEventStore, func()) {
		store, openErr := sqlite.Open(openCtx, dbPath)
		if openErr != nil {
			fmt.Fprintf(os.Stderr, "okt update: sqlite open %s failed, activity rows skipped: %v\n", dbPath, openErr)
			return nil, func() {}
		}
		return store, func() { _ = store.Close() }
	}
	return nil
}

// updateInputs is the resolved flag set runUpdate consumes. Kept as a
// struct (rather than two booleans threaded through the signature) so
// future flags (--channel beta, --prerelease) drop in without
// breaking call sites.
type updateInputs struct {
	Check        bool
	Yes          bool
	SkipDefaults bool
}

// runUpdate is the headless-friendly entry point: resolve latest,
// compare, optionally confirm + swap. Returns the JSON envelope
// payload. --check short-circuits before any side-effect.
func runUpdate(ctx context.Context, c updateClient, inputs updateInputs) (any, error) {
	current, latest, err := resolveUpdateVersions(ctx, c)
	if err != nil {
		return nil, err
	}
	action := updateAction(current, latest)
	if inputs.Check || action == "noop" {
		return updateStatus(current, latest, action), nil
	}
	if err := validateUpdateTarget(current, latest); err != nil {
		return nil, err
	}
	if err := confirmUpdate(ctx, current, latest, inputs.Yes); err != nil {
		return nil, err
	}
	asset, err := updater.AssetName(currentGOOS, goruntime.GOARCH)
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, err.Error(), nil)
	}
	stagedPath, verified, err := stageUpdateBinary(ctx, c, current, latest, asset)
	if err != nil {
		return nil, err
	}
	swapped := false
	defer func() {
		if !swapped {
			_ = os.Remove(stagedPath)
		}
	}()
	cleanupEventStore := openUpdateEventStore(ctx, &c)
	defer cleanupEventStore()
	if err := validateStagedUpdate(ctx, c, current, latest, stagedPath); err != nil {
		return nil, err
	}
	backupPath, err := runUpdateBackup(ctx, c)
	if err != nil {
		return nil, err
	}
	if err := updater.SwapStagedBinary(stagedPath, c.BinaryPath); err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.swap_binary"), c.BinaryPath, err.Error()), nil)
	}
	swapped = true
	return finishUpdate(ctx, c, inputs, current, latest, action, verified, backupPath)
}

func openUpdateEventStore(ctx context.Context, client *updateClient) func() {
	if client.EventStoreFactory == nil {
		return func() {}
	}
	store, cleanup := client.EventStoreFactory(ctx)
	if store != nil {
		client.EventStore = store
	}
	if cleanup == nil {
		return func() {}
	}
	return cleanup
}

func resolveUpdateVersions(ctx context.Context, c updateClient) (string, string, error) {
	if current := strings.TrimSpace(c.Current); current == "" || current == "dev" {
		return "", "", domain.NewError(domain.ErrValidation, t("cli.update.err.dev_build"), nil)
	}
	if currentGOOS == "windows" {
		return "", "", domain.NewError(domain.ErrUpdateFailed, t("cli.update.err.windows_unsupported"), nil)
	}
	latest, err := c.Fetcher.Latest(ctx)
	if err != nil {
		return "", "", domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.fetch_latest"), err.Error()), nil)
	}
	return normalizeVersion(c.Current), normalizeVersion(latest), nil
}

func updateAction(current, latest string) string {
	if current == latest {
		return "noop"
	}
	return "upgrade"
}

func updateStatus(current, latest, action string) map[string]any {
	code := "update_not_required"
	if action == "upgrade" {
		code = "update_available"
	}
	return map[string]any{"code": code, "current": current, "latest": latest, "action": action, "applied": false}
}

func validateUpdateTarget(current, latest string) error {
	// Strict release policy runs before confirmation and before any release byte
	// is fetched, so a rollback target or legacy release cannot reach the swap.
	if err := releaseverify.RequireUpgrade(current, latest); err != nil {
		return domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.downgrade_blocked_fmt"), latest, current), nil)
	}
	if err := releaseverify.RequireSignedTarget(latest); err != nil {
		return domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.unsigned_release_fmt"), latest, releaseverify.LegacyChecksumCutoff), nil)
	}
	return nil
}

func confirmUpdate(ctx context.Context, current, latest string, yes bool) error {
	if yes {
		return nil
	}
	if !stdinIsTTY() {
		return domain.NewError(domain.ErrValidation, t("cli.update.picker.no_tty"), nil)
	}
	confirmed, err := runUpdateConfirm(ctx, current, latest)
	if err != nil {
		return err
	}
	if !confirmed {
		return domain.NewError(domain.ErrValidation, t("cli.update.picker.aborted"), nil)
	}
	return nil
}

func stageUpdateBinary(ctx context.Context, c updateClient, current, latest, asset string) (string, releaseverify.Result, error) {
	if c.ReleaseVerifier == nil {
		return "", releaseverify.Result{}, domain.NewError(domain.ErrUpdateFailed, t("cli.update.err.verifier_unavailable"), nil)
	}
	archiveBytes, verified, err := downloadAndVerifyUpdate(ctx, c, current, latest, asset)
	if err != nil {
		return "", releaseverify.Result{}, err
	}
	binary, err := lifecycle.ExtractBinary(bytes.NewReader(archiveBytes), currentGOOS, lifecycle.BinaryName())
	if err != nil {
		return "", releaseverify.Result{}, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.extract_asset"), lifecycle.BinaryName(), err.Error()), nil)
	}
	stagedPath, err := updater.StageBinary(c.BinaryPath, bytes.NewReader(binary))
	if err != nil {
		return "", releaseverify.Result{}, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.stage_binary_fmt"), err.Error()), nil)
	}
	return stagedPath, verified, nil
}

func downloadAndVerifyUpdate(ctx context.Context, c updateClient, current, latest, asset string) ([]byte, releaseverify.Result, error) {
	archiveBytes, err := updater.DownloadAsset(ctx, c.Downloader, latest, asset)
	if err != nil {
		return nil, releaseverify.Result{}, domain.NewError(domain.ErrUpdateFailed, err.Error(), nil)
	}
	metadata := make(map[string][]byte, 5)
	for _, name := range []string{releaseverify.ChecksumsName, releaseverify.ManifestName(latest), releaseverify.ManifestBundleName(latest), releaseverify.ChecksumsBundleName(latest), releaseverify.ProvenanceBundleName(latest)} {
		data, err := updater.DownloadAsset(ctx, c.Downloader, latest, name)
		if err != nil {
			return nil, releaseverify.Result{}, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.fetch_release_metadata_fmt"), name, err.Error()), nil)
		}
		metadata[name] = data
	}
	verified, err := c.ReleaseVerifier(ctx, releaseverify.Release{
		Repository:       updateRepo,
		Version:          latest,
		ArchiveName:      asset,
		Archive:          archiveBytes,
		Checksums:        metadata[releaseverify.ChecksumsName],
		Manifest:         metadata[releaseverify.ManifestName(latest)],
		ManifestBundle:   metadata[releaseverify.ManifestBundleName(latest)],
		ChecksumsBundle:  metadata[releaseverify.ChecksumsBundleName(latest)],
		ProvenanceBundle: metadata[releaseverify.ProvenanceBundleName(latest)],
	})
	if err != nil {
		return nil, releaseverify.Result{}, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.signature_verification_failed_fmt"), err.Error()), map[string]any{"reason": "release_verification_failed", "current": current, "latest": latest, "binary_path": c.BinaryPath, "cause": err.Error()})
	}
	gotSum := fmt.Sprintf("%x", sha256.Sum256(archiveBytes))
	if !strings.EqualFold(gotSum, verified.ArchiveSHA256) {
		return nil, releaseverify.Result{}, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.checksum_mismatch"), verified.ArchiveSHA256, gotSum), nil)
	}
	return archiveBytes, verified, nil
}

func validateStagedUpdate(ctx context.Context, c updateClient, current, latest, stagedPath string) error {
	if c.Validator == nil {
		return nil
	}
	if strings.TrimSpace(c.ConfigPath) == "" {
		return domain.NewError(domain.ErrUpdateFailed, "staged config validation requires a resolved config path; binary unchanged", map[string]any{
			"reason":      "config_validation_path_unavailable",
			"current":     current,
			"latest":      latest,
			"binary_path": c.BinaryPath,
			"staged_path": stagedPath,
		})
	}
	result, err := c.Validator(ctx, stagedPath, c.ConfigPath)
	if err != nil {
		return domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.config_validation_exec_fmt"), err.Error()), map[string]any{"reason": "config_validation_exec_failed", "current": current, "latest": latest, "binary_path": c.BinaryPath, "staged_path": stagedPath, "cause": err.Error()})
	}
	if result.OK {
		emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateHealthCheckPassed, map[string]any{"from_version": current, "to_version": latest, "binary_path": c.BinaryPath, "staged_path": stagedPath})
		return nil
	}
	firstKind := updateValidatorFirstKind(result.Errors)
	emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateHealthCheckFailed, map[string]any{"from_version": current, "to_version": latest, "staged_path": stagedPath, "validator_error_count": len(result.Errors), "validator_first_error_kind": firstKind, "validator_raw_excerpt": string(result.RawOutput)})
	emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateSwapAborted, map[string]any{"from_version": current, "to_version": latest, "reason": "config_validation_failed", "validator_error_count": len(result.Errors)})
	return domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.config_validation_failed_fmt"), len(result.Errors), firstKind), map[string]any{"reason": "config_validation_failed", "current": current, "latest": latest, "binary_path": c.BinaryPath, "staged_path": stagedPath, "errors": result.Errors, "validator_raw": string(result.RawOutput)})
}

func updateValidatorFirstKind(errors []map[string]any) string {
	if len(errors) == 0 {
		return ""
	}
	firstKind, _ := errors[0]["kind"].(string)
	return firstKind
}

func runUpdateBackup(ctx context.Context, c updateClient) (string, error) {
	backupRunner := c.Backup
	if c.BackupFactory != nil {
		runner, err := c.BackupFactory(ctx)
		if err != nil {
			return "", domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.backup_failed_fmt"), err.Error()), map[string]any{"reason": "backup_failed", "cause": err.Error()})
		}
		backupRunner = runner
	}
	if backupRunner == nil {
		return "", nil
	}
	path, err := backupRunner.Run(ctx)
	if err != nil {
		return "", domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf(t("cli.update.err.backup_failed_fmt"), err.Error()), map[string]any{"reason": "backup_failed", "cause": err.Error()})
	}
	return path, nil
}

func finishUpdate(ctx context.Context, c updateClient, inputs updateInputs, current, latest, action string, verified releaseverify.Result, backupPath string) (map[string]any, error) {

	emitHealthCheckEvent(ctx, c.EventStore, domain.EventTypeUpdateSwapCompleted, map[string]any{
		"from_version": current,
		"to_version":   latest,
		"binary_path":  c.BinaryPath,
		"backup_path":  backupPath,
	})
	defaultsRefreshed := false
	if !inputs.SkipDefaults && c.DefaultsRefresher != nil {
		if err := c.DefaultsRefresher(ctx, c.BinaryPath); err != nil {
			manualCommand := updateDefaultsManualCommandForConfig(c.DefaultsRefreshConfigPath)
			return nil, domain.NewError(domain.ErrUpdateFailed, fmt.Sprintf("binary update applied but defaults refresh failed: %s; run `%s` to repair", err.Error(), manualCommand), map[string]any{
				"reason":         "defaults_refresh_failed",
				"current":        current,
				"latest":         latest,
				"action":         action,
				"applied":        true,
				"binary_path":    c.BinaryPath,
				"backup_path":    backupPath,
				"manual_command": manualCommand,
				"config_path":    c.DefaultsRefreshConfigPath,
				"cause":          err.Error(),
			})
		}
		defaultsRefreshed = true
	}

	return map[string]any{
		"code":                     "update_completed",
		"current":                  current,
		"latest":                   latest,
		"action":                   action,
		"applied":                  true,
		"binary_path":              c.BinaryPath,
		"backup_path":              backupPath,
		"defaults_refreshed":       defaultsRefreshed,
		"defaults_refresh_skipped": inputs.SkipDefaults,
		"signature_verified":       true,
		"source_commit":            verified.SourceCommit,
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

var defaultUpdateClientFactory = defaultUpdateClient

// defaultUpdateClient builds the production wiring: HTTP-backed
// updater.LatestFetcher + updater.AssetDownloader pointing at the GitHub releases API,
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
		Fetcher:           &updater.GitHubLatestFetcher{Repo: updateRepo, HTTP: hc},
		Downloader:        &updater.GitHubAssetDownloader{Repo: updateRepo, HTTP: hc},
		Current:           version,
		BinaryPath:        bin,
		DefaultsRefresher: defaultUpdateDefaultsRefresher,
		ReleaseVerifier:   defaultReleaseVerifier,
	}, nil
}

// defaultReleaseVerifier resolves the Sigstore trust anchors lazily — only
// once an update is actually about to be applied — and refuses to continue if
// they cannot be refreshed. The cache lives under the user's state dir so a
// repeat update does not need a fresh TUF round trip, and so the refreshed
// metadata is never written to a shared temp path.
func defaultReleaseVerifier(ctx context.Context, rel releaseverify.Release) (releaseverify.Result, error) {
	stateDir, err := paths.StateDir()
	if err != nil {
		return releaseverify.Result{}, fmt.Errorf("resolve sigstore root cache: %w", err)
	}
	trust, err := releaseverify.TrustedRoot(ctx, filepath.Join(stateDir, "sigstore"))
	if err != nil {
		return releaseverify.Result{}, err
	}
	verifier, err := releaseverify.New(trust, rel.Repository)
	if err != nil {
		return releaseverify.Result{}, err
	}
	return verifier.Verify(rel)
}

func defaultUpdateDefaultsRefresher(ctx context.Context, binaryPath string) error {
	return runUpdateDefaultsRefreshCommand(ctx, binaryPath, updateDefaultsRefreshArgs("")...)
}

func updateDefaultsRefresherForConfigPath(configPath string) updateDefaultsRefresherFn {
	return func(ctx context.Context, binaryPath string) error {
		return runUpdateDefaultsRefreshCommand(ctx, binaryPath, updateDefaultsRefreshArgs(configPath)...)
	}
}

func updateDefaultsRefreshArgs(configPath string) []string {
	args := []string{}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	return append(args, "config", "refresh-defaults")
}

func runUpdateDefaultsRefreshCommand(ctx context.Context, binaryPath string, args ...string) error {
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = os.Environ()
	stdout := newCappedOutputBuffer(updateDefaultsRefreshOutputLimit)
	stderr := newCappedOutputBuffer(updateDefaultsRefreshOutputLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		parts := []string{err.Error()}
		if out := strings.TrimSpace(stdout.String()); out != "" {
			parts = append(parts, "stdout: "+out)
		}
		if out := strings.TrimSpace(stderr.String()); out != "" {
			parts = append(parts, "stderr: "+out)
		}
		return errors.New(strings.Join(parts, "; "))
	}
	return nil
}

const updateDefaultsRefreshOutputLimit = 64 << 10

type cappedOutputBuffer struct {
	limit     int
	buf       bytes.Buffer
	truncated int
}

func newCappedOutputBuffer(limit int) *cappedOutputBuffer {
	return &cappedOutputBuffer{limit: limit}
}

func (b *cappedOutputBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		b.truncated += len(p)
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			_, _ = b.buf.Write(p)
			return len(p), nil
		}
		_, _ = b.buf.Write(p[:remaining])
	}
	if len(p) > remaining {
		b.truncated += len(p) - max(remaining, 0)
	}
	return len(p), nil
}

func (b *cappedOutputBuffer) String() string {
	out := b.buf.String()
	if b.truncated > 0 {
		out += fmt.Sprintf("\n[okt update: subprocess output truncated after %d bytes; %d bytes suppressed]", b.limit, b.truncated)
	}
	return out
}

// defaultUpdateValidator runs the staged binary's `okt config validate
// --config <configPath>` and parses its JSON envelope. A
// non-zero exit code from the validator is the normal failure path
// (returned as result.OK=false, not as a Go error) — only spawn
// failures, context cancellation, signal termination, or unparseable output
// produce an error here. The validator inherits the parent's env so the
// staged binary sees the same `OMAKITEN_HOME` / XDG state the live binary
// would on next launch.
//
// Stdout and stderr are captured into SEPARATE buffers — the staged
// binary's `emitBundleWarnings` writes to stderr while the JSON
// envelope lands on stdout, and a shared buffer would prepend the
// warning text to the JSON and break `json.Unmarshal` with an
// `invalid character` error. Stderr is mirrored to the parent's
// stderr after the call so warnings stay visible without poisoning
// the parse.
func defaultUpdateValidator(ctx context.Context, binaryPath, configPath string) (updateValidatorResult, error) {
	cmd := exec.CommandContext(ctx, binaryPath, "config", "validate", "--config", configPath)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	mirrorValidatorStderr(stderr.Bytes())
	if ctxErr := ctx.Err(); ctxErr != nil {
		return updateValidatorResult{OK: false, RawOutput: stdout.Bytes()}, fmt.Errorf("validator execution: %w", ctxErr)
	}

	var exitErr *exec.ExitError
	exitedNonZero := errors.As(runErr, &exitErr)
	if exitedNonZero {
		if exitErr.ProcessState != nil && exitErr.ExitCode() < 0 {
			return updateValidatorResult{OK: false, RawOutput: stdout.Bytes()}, fmt.Errorf("validator terminated by signal: %w", runErr)
		}
		// Non-zero exit is the documented validator-fail path;
		// surface OK=false with parsed errors rather than treating
		// it as exec infrastructure breakage.
		runErr = nil
	}
	if runErr != nil {
		return updateValidatorResult{OK: false, RawOutput: stdout.Bytes()}, runErr
	}

	result, err := parseValidatorOutput(stdout.Bytes(), exitedNonZero)
	if exitedNonZero {
		result.OK = false
	}
	return result, err
}

func mirrorValidatorStderr(buf []byte) {
	if len(buf) == 0 {
		return
	}
	const stderrMirrorCap = 64 << 10
	if len(buf) > stderrMirrorCap {
		_, _ = os.Stderr.Write(buf[:stderrMirrorCap])
		fmt.Fprintf(os.Stderr, "\n[okt update: validator stderr truncated after %d bytes; %d more bytes suppressed]\n", stderrMirrorCap, len(buf)-stderrMirrorCap)
		return
	}
	_, _ = os.Stderr.Write(buf)
}

func parseValidatorOutput(output []byte, exitedNonZero bool) (updateValidatorResult, error) {
	raw := bytes.TrimSpace(output)
	if len(raw) == 0 {
		if exitedNonZero {
			return updateValidatorResult{OK: false}, nil
		}
		return updateValidatorResult{OK: false}, fmt.Errorf("validator produced no output")
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

// normalizeVersion strips a leading "v" so the github API tag
// ("v0.19.0") and the build-injected --version ("0.19.0") compare
// equal. Empty strings flow through untouched so "dev" / "" stays as
// the caller wrote it.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimPrefix(v, "v")
}

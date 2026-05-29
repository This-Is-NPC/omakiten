package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	hookactions "omakiten/internal/hooks/actions"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
	"omakiten/internal/tui"
)

func newTUICommand(opts *runtimeOptions, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: opts.t("cli.tui.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.Context(), opts, version)
		},
	}
}

func runTUI(ctx context.Context, opts *runtimeOptions, version string) error {
	rt, err := opts.open(ctx, true)
	if err != nil {
		emitTUIHealthCheckFailedFromOpenError(ctx, opts, err)
		return err
	}
	defer rt.close()

	ctx = activity.WithAgent(ctx, "tui", "tui", "human", "")
	ctx = rt.WithActivityRepo(ctx)

	project, err := opts.resolveProject(ctx, rt.store)
	if err != nil {
		// Without an explicit --project / --project-id, an unresolvable CWD
		// is not an error — it is the trigger for the multi-project Home
		// screen. Explicit flags must still 404 loudly so typos are caught.
		if opts.projectID == 0 && opts.project == "" && isProjectNotFoundError(err) {
			project = domain.ProjectContext{}
		} else {
			return err
		}
	}
	// Boot-time health check (#365 AC 5). `opts.open(_, true)`
	// already ran MigrateLayout + EnsureDefaultFiles + LoadBundle +
	// ValidateBundle and wrapped any failure with the structured
	// envelope at root.go:299. The bundle handle below is consumed
	// only for the active-theme snapshot guard — if `opts.open`
	// returned a valid runtime, the bundle is loadable, so a fresh
	// LoadBundle here would be redundant.
	bundle, err := config.LoadBundle(rt.configPath)
	if err != nil {
		// Defensive: unreachable in the current `opts.open` flow,
		// kept as guard against future open() refactors that might
		// elide the in-line LoadBundle. Surfaces the same envelope
		// the wrap-in-open path emits so the user sees one shape.
		firstKind := classifyValidationError(err)
		return domain.NewError(
			domain.ErrConfigInvalid,
			fmt.Sprintf(t("cli.tui.err.config_validation_failed_fmt"), 1, firstKind),
			buildValidateFailureDetails(rt.configPath, err, nil),
		)
	}
	snap := rt.activeSnapshot()
	if err := snap.ThemeError(); err != nil {
		// Theme snapshot failures aren't caught by `opts.open`'s
		// LoadBundle path — the snapshot is built from the loaded
		// bundle, and an unresolvable theme slug surfaces here as
		// a distinct boot guard. Reuse the same envelope shape so
		// the user sees consistent kind + remediation copy.
		warnings := extractBundleWarnings(bundle)
		firstKind := classifyValidationError(err)
		return domain.NewError(
			domain.ErrConfigInvalid,
			fmt.Sprintf(t("cli.tui.err.config_validation_failed_fmt"), 1, firstKind),
			buildValidateFailureDetails(rt.configPath, err, warnings),
		)
	}
	theme := snap.Theme()

	bundleStore := configstore.New()
	editor := app.NewBundleEditor(bundleStore, rt.configPath)
	model, err := tui.NewModel(ctx, project, tui.Repositories{
		Tasks:        rt.store,
		Projects:     rt.store,
		Workflow:     app.NewWorkflowServiceFromStore(rt.store, rt.activeRegistry(), rt.activeSnapshot()),
		Comments:     rt.store,
		Dependencies: rt.store,
		Entries:      rt.store,
		Tags:         rt.store,
		Editor:       editor,
		BundleStore:  bundleStore,
		EntityFiles:  bundleStore,
		Slugger:      bundleStore,
		ActivityLogs: rt.store,
		Events:       rt.store,
		Metrics:      app.NewMetricsService(rt.store),
		Orphans:      rt.store,
		Plans:        rt.store,
		Search:       app.NewSearchService(rt.store, rt.store),
		Checkpointer: rt.store,
		DispatchCommand: func(ctx context.Context, args []string) ([]byte, error) {
			cmd := NewRootCommand(version)
			cmd.SetContext(ctx)
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(args)
			err := cmd.Execute()
			return buf.Bytes(), err
		},
		ConfigPath:   rt.configPath,
		DBPath:       rt.dbPath,
		Version:      version,
		RepoLocalDir: rt.repoLocalDir,
		Cache:        rt.cache,
		ProjectID:    rt.projectID,
		Catalog:      rt.activeSnapshot().Catalog(config.SurfaceTUI),
	}, theme, token.NewCounter(), bundle.Config.TUI.TokenBadge, bundle.Config.EffectivePriorities(), bundle.Config.EffectiveSeverities(), tui.NotificationBinding{
		Notifications: bundle.Notifications,
	})
	if err != nil {
		return err
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	if rt.notificationAction != nil {
		rt.notificationAction.SetSender(teaNotificationSender{program: program})
	}
	finalModel, runErr := program.Run()
	// The shell wrapper installed by install.sh / install.ps1 reads the
	// path written here and `cd`s the parent shell after the TUI exits.
	// Without the wrapper this is a silent no-op; the TUI itself never
	// changes the parent shell's CWD (it cannot).
	if final, ok := finalModel.(tui.Model); ok {
		if root := final.LastProjectRoot(); root != "" {
			_ = writeOktCDPath(root)
		}
	}
	return runErr
}

type teaNotificationSender struct {
	program *tea.Program
}

func (s teaNotificationSender) SendNotification(msg hookactions.NotificationShowMsg) {
	s.program.Send(msg)
}

// isProjectNotFoundError returns true when the resolver signalled that the
// current working directory is not inside any registered project. We unwrap
// the domain CodedError to compare codes rather than match on message text.
func isProjectNotFoundError(err error) bool {
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		return coded.Code == domain.ErrProjectNotFound
	}
	return false
}

// writeOktCDPath writes the absolute project root path to the channel the
// shell wrapper reads after the TUI exits. Resolution order, mirroring the
// wrapper itself: $OKT_CD_FILE → $XDG_RUNTIME_DIR/okt-cd → $TMPDIR/okt-cd-$UID
// → /tmp/okt-cd-$UID. Best-effort: an I/O failure here is not surfaced to
// the user because the wrapper treats a missing file as "no cd needed".
func writeOktCDPath(root string) error {
	target := oktCDPath()
	if target == "" {
		return nil
	}
	return os.WriteFile(target, []byte(root+"\n"), 0o600)
}

// emitTUIHealthCheckFailedFromOpenError records a tui.healthcheck.failed
// row when opts.open returns a config-invalid coded error (#369 AC 4).
// The on-disk store is opened from scratch because opts.open closes
// the store before returning the wrapping error; the helper is
// best-effort and the failure surface is unaffected when the activity
// path is unavailable.
func emitTUIHealthCheckFailedFromOpenError(ctx context.Context, opts *runtimeOptions, openErr error) {
	var coded *domain.CodedError
	if !errors.As(openErr, &coded) || coded.Code != domain.ErrConfigInvalid {
		return
	}
	dbPath, dbErr := opts.resolvedDBPath()
	if dbErr != nil {
		return
	}
	store, storeErr := sqlite.Open(ctx, dbPath)
	if storeErr != nil {
		return
	}
	defer store.Close()

	payload := map[string]any{}
	if cfgPath, _ := coded.Details["path"].(string); cfgPath != "" {
		payload["config_path"] = cfgPath
	}
	count, firstKind := summariseValidationErrors(coded.Details["errors"])
	// Caller chooses the audit-row default when the wrapper could
	// not enumerate any structured errors: the bundle clearly broke
	// in some way (we are in the config-invalid branch) so emit
	// `validator_error_count: 1` as the "we saw an error but cannot
	// say which" floor. The historic 1 lived inside
	// summariseValidationErrors and could not be distinguished from
	// "exactly one error was recorded".
	if count == 0 {
		count = 1
	}
	payload["validator_error_count"] = count
	if firstKind != "" {
		payload["validator_first_error_kind"] = firstKind
	}
	emitHealthCheckEvent(ctx, store, domain.EventTypeTUIHealthCheckFailed, payload)
}

// summariseValidationErrors extracts (count, first-error-kind) from
// the `details.errors` payload regardless of whether it landed as the
// hand-built `[]map[string]any` shape (in-process wrap) or the
// JSON-roundtripped `[]any` shape (activity-store roundtrip, hook
// payload). The TUI helper used to take the `[]map[string]any`
// branch only, so a roundtripped envelope silently fell back to
// count=1 and no kind — Primitive Obsession on `map[string]any`.
//
// Returns (0, "") when no structured errors could be enumerated;
// callers decide whether to emit a count=1 floor or skip the field.
// Pre-fix the helper baked the count=1 default in and consumers
// could not distinguish "no enumerable errors" from "exactly one
// error was recorded".
func summariseValidationErrors(raw any) (int, string) {
	switch errs := raw.(type) {
	case []map[string]any:
		if len(errs) == 0 {
			return 0, ""
		}
		kind, _ := errs[0]["kind"].(string)
		return len(errs), kind
	case []any:
		if len(errs) == 0 {
			return 0, ""
		}
		if m, ok := errs[0].(map[string]any); ok {
			kind, _ := m["kind"].(string)
			return len(errs), kind
		}
		return len(errs), ""
	}
	return 0, ""
}

func oktCDPath() string {
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


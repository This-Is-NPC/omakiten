package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"omakiten/internal/activity"
	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
)

func newTUICommand(opts *runtimeOptions, version string, run func(context.Context, agentruntime.Session) error) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: opts.t("cli.tui.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.Context(), opts, version, run)
		},
	}
}

func runTUI(ctx context.Context, opts *runtimeOptions, version string, run func(context.Context, agentruntime.Session) error) error {
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
	snap := rt.activeSnapshot()
	if err := snap.ThemeError(); err != nil {
		// Theme snapshot failures aren't caught by `opts.open`'s
		// LoadBundle path — the snapshot is built from the loaded
		// bundle, and an unresolvable theme slug surfaces here as
		// a distinct boot guard. Reuse the same envelope shape so
		// the user sees consistent kind + remediation copy.
		warnings := extractBundleWarnings(config.Bundle{Warnings: snap.Warnings()})
		firstKind := classifyValidationError(err)
		return domain.NewError(
			domain.ErrConfigInvalid,
			fmt.Sprintf(t("cli.tui.err.config_validation_failed_fmt"), 1, firstKind),
			buildValidateFailureDetails(rt.configPath, err, warnings),
		)
	}
	if run == nil {
		return fmt.Errorf("interactive runner is not installed")
	}
	return run(ctx, agentruntime.Session{CacheProjectID: rt.projectID, Store: rt.store, Cache: rt.cache, Project: project, ConfigPath: rt.configPath, DBPath: rt.dbPath, RepoLocalDir: rt.repoLocalDir, Version: version, Snapshot: snap})
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

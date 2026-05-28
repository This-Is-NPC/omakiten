package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
)

// newLogsCommand wires `okt logs` — the unified Logs inspector read
// surface. The command shells `internal/sqlite.Store.ListEvents` with a
// caller-built `domain.EventFilter`, then projects each row into a
// 5-field JSON object whose `detail` column is `domain.SummarizeEvent`
// applied verbatim. The set of categories the CLI accepts mirrors
// `domain.KnownEventCategories` exactly so the chip vocabulary stays
// canonical across the CLI / TUI / MCP triad.
//
// Default scope: last `Snapshot.LogsWindowDays()` of every category for
// the resolved project. `--since` overrides the time floor and
// `--category` (repeatable, comma-separated) restricts the category
// set; the resolved set ANDs across both axes the same way SQLite
// expects via `EventFilter`.
//
// Breaking change vs the legacy `activity_logs` JSON shape (umbrella
// #320 D3): rows now carry the EventRow projection (`event_type`,
// `entity_type`, `author_type`, `summary`) instead of the
// `ActivityLog`/`source` shape. The CHANGELOG note under
// `feat(cli)!` flags the break for downstream tooling.
func newLogsCommand(opts *runtimeOptions) *cobra.Command {
	var (
		categoryFlags []string
		sinceFlag     string
		limitFlag     int
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: opts.t("cli.logs.short"),
		Long:  opts.t("cli.logs.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				categories, err := parseLogCategories(categoryFlags)
				if err != nil {
					return nil, err
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}

				since, err := resolveLogSince(sinceFlag, rt.activeSnapshot(), time.Now)
				if err != nil {
					return nil, err
				}

				filter := domain.EventFilter{
					ProjectID:  project.ID,
					Categories: categories,
					Since:      since,
					Limit:      limitFlag,
				}

				rows, err := rt.store.ListEvents(ctx, filter)
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"project": project,
					"events":  projectLogRows(rows),
				}, nil
			})
		},
	}

	cmd.Flags().StringSliceVarP(&categoryFlags, "category", "c", nil, opts.t("cli.logs.flag.category"))
	cmd.Flags().StringVar(&sinceFlag, "since", "", opts.t("cli.logs.flag.since"))
	cmd.Flags().IntVarP(&limitFlag, "limit", "n", 0, opts.t("cli.logs.flag.limit"))
	return cmd
}

// logRow is the JSON shape `okt logs` emits per event. The five
// user-facing columns (time · type · entity · who · detail) are
// pinned to stable JSON keys so downstream tooling can `jq` them
// without sniffing the EventRow projection. `summary` is the
// `SummarizeEvent` output verbatim — the AC #6 contract.
type logRow struct {
	Time        string `json:"time"`
	EventType   string `json:"event_type"`
	EntityType  string `json:"entity_type"`
	EntityID    int64  `json:"entity_id"`
	AuthorType  string `json:"author_type"`
	Category    string `json:"category"`
	Summary     string `json:"summary"`
	Source      string `json:"source,omitempty"`
	Status      string `json:"status,omitempty"`
	DurationMs  int    `json:"duration_ms,omitempty"`
	AgentModel  string `json:"agent_model,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
}

// projectLogRows projects each EventRow into the JSON-stable logRow
// shape, deriving the category from `domain.EventCategoryOf` and the
// `summary` column from `domain.SummarizeEvent`. Returns a non-nil
// empty slice when the store yielded no rows so the JSON envelope
// always carries `"events":[]` instead of `null`.
func projectLogRows(rows []domain.EventRow) []logRow {
	out := make([]logRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, logRow{
			Time:        r.CreatedAt,
			EventType:   r.EventType,
			EntityType:  r.EntityType,
			EntityID:    r.EntityID,
			AuthorType:  r.AuthorType,
			Category:    string(domain.EventCategoryOf(r.EventType)),
			Summary:     domain.SummarizeEvent(r),
			Source:      r.Source,
			Status:      r.Status,
			DurationMs:  r.DurationMs,
			AgentModel:  r.AgentModel,
			ProjectSlug: r.ProjectSlug,
		})
	}
	return out
}

// parseLogCategories normalises the repeatable `--category` flag into
// the canonical `domain.EventCategory` set. Each input value is
// trimmed, lowercased, and split on comma so callers can write either
// `--category task --category plan` or `--category task,plan`. The
// special token `all` selects every known category; empty input
// returns nil (no category filter) to keep the zero-value semantics
// `EventFilter` documents intact.
//
// Validation: unknown category names yield a typed
// `validation_error` so the JSON envelope surfaces the bad token to
// the caller instead of silently dropping it. The accepted set is
// `domain.KnownEventCategories` — adding a new category to the
// catalog automatically extends the CLI vocabulary.
func parseLogCategories(values []string) ([]domain.EventCategory, error) {
	if len(values) == 0 {
		return nil, nil
	}
	known := knownCategorySet()
	seen := map[domain.EventCategory]struct{}{}
	out := []domain.EventCategory{}
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			token := strings.ToLower(strings.TrimSpace(part))
			if token == "" {
				continue
			}
			if token == "all" {
				// `all` short-circuits the filter — no category
				// predicate becomes "every category" in
				// ListEvents which mirrors the chip behaviour.
				return nil, nil
			}
			cat := domain.EventCategory(token)
			if _, ok := known[cat]; !ok {
				return nil, domain.NewError(
					domain.ErrValidation,
					t("cli.err.logs_invalid_category"),
					map[string]any{
						"category": token,
						"allowed":  knownCategoryNames(),
					},
				)
			}
			if _, dup := seen[cat]; dup {
				continue
			}
			seen[cat] = struct{}{}
			out = append(out, cat)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// knownCategorySet returns the lookup set used by parseLogCategories
// to reject typos. Built from domain.KnownEventCategories so adding a
// new category to the catalog auto-extends the CLI's accepted
// vocabulary without a parallel edit here.
func knownCategorySet() map[domain.EventCategory]struct{} {
	out := make(map[domain.EventCategory]struct{}, len(domain.KnownEventCategories))
	for _, c := range domain.KnownEventCategories {
		out[c] = struct{}{}
	}
	return out
}

// knownCategoryNames returns the accepted category vocabulary as a
// sorted string slice — surfaced under the `allowed` detail of the
// `validation_error` envelope so agents reading the JSON failure can
// retry with a correct token without re-discovering the catalogue.
func knownCategoryNames() []string {
	out := make([]string, 0, len(domain.KnownEventCategories)+1)
	out = append(out, "all")
	for _, c := range domain.KnownEventCategories {
		out = append(out, string(c))
	}
	sort.Strings(out)
	return out
}

// resolveLogSince applies the `--since` flag if supplied and falls
// back to the `Snapshot.LogsWindowDays()` default otherwise. Empty
// snapshot pointer (rare bootstrap window before materialize) and a
// zero LogsWindowDays both degrade to "no time floor" so the command
// still returns rows in those edge paths.
//
// The `--since` value parses through `time.ParseDuration` so callers
// can write `24h`, `30m`, or longer composites. Plain day strings
// like `7d` are accepted as an extra ergonomic shorthand because
// `time.ParseDuration` itself rejects the `d` unit. Anything that
// fails both paths surfaces as a typed `validation_error`.
//
// The `now` callback is injected so tests can substitute a
// deterministic clock (internal/testfakes/clock.Fake.Now) instead of
// snapshotting time.Now() and comparing against a tolerance window.
// The production caller passes time.Now verbatim so the wall-clock
// behaviour of `okt logs --since` is unchanged. A nil `now` is
// treated as time.Now so callers without a clock preference (mostly
// future helper packages) need not branch at every call site.
func resolveLogSince(flag string, snap interface{ LogsWindowDays() time.Duration }, now func() time.Time) (time.Time, error) {
	if now == nil {
		now = time.Now
	}
	nowUTC := now().UTC()
	if flag != "" {
		dur, err := parseLogDuration(flag)
		if err != nil {
			return time.Time{}, domain.NewError(
				domain.ErrValidation,
				t("cli.err.logs_invalid_since"),
				map[string]any{"since": flag, "error": err.Error()},
			)
		}
		if dur <= 0 {
			return time.Time{}, nil
		}
		return nowUTC.Add(-dur), nil
	}
	if snap == nil {
		return time.Time{}, nil
	}
	win := snap.LogsWindowDays()
	if win <= 0 {
		return time.Time{}, nil
	}
	return nowUTC.Add(-win), nil
}

// parseLogDuration accepts a `time.ParseDuration` string with an
// extra `d` (days) suffix that the stdlib rejects. Splits on a single
// trailing `d` and converts to hours so the rest of the value can
// still feed through ParseDuration if needed in future. Keeps the
// implementation deliberately small — the umbrella scope is the
// inspector, not a calendar arithmetic library.
func parseLogDuration(value string) (time.Duration, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if strings.HasSuffix(v, "d") {
		var days int
		if _, err := fmt.Sscanf(v, "%dd", &days); err != nil {
			return 0, err
		}
		if days < 0 {
			return 0, fmt.Errorf("negative days: %s", v)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(v)
}

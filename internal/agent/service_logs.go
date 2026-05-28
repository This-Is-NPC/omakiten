package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// logsSinceLayout is the ISO-8601 timestamp shape the LogsResponse
// echoes for window_since. Matches SQLite's CURRENT_TIMESTAMP layout
// after a UTC normalization so callers comparing window_since to
// row.created_at stay in the same lexicographic space.
const logsSinceLayout = "2006-01-02T15:04:05Z"

// ListLogs implements the `logs.list` MCP tool. It returns rows from
// the unified events log scoped to the active project and shaped with
// a rendered `summary` per row so agents see human-readable text
// without parsing the payload JSON.
//
// Defaults are applied here so MCP callers can pass nothing and still
// get a useful 30-day window of every event category:
//
//   - Empty categories  → no category filter (every event_type).
//   - Empty since       → time floor = now - Snapshot.LogsWindowDays.
//   - Limit <= 0        → SQL layer caps at MaxListEventsLimit (10000).
//   - Order ""          → "desc" (newest first).
//
// Validation is intentionally permissive — unknown categories are
// silently dropped (the SQL layer returns no rows for them), an
// unparseable `since` surfaces as a validation error so the caller
// can correct the input rather than receive a silent wrong-window.
func (s *Service) ListLogs(ctx context.Context, input ListLogsInput) (ListLogsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ListLogsResponse{}, err
	}

	categories, err := normalizeLogsCategories(input.Categories)
	if err != nil {
		return ListLogsResponse{}, err
	}

	since, err := resolveLogsSince(input.Since, s.snapshot, s.nowFunc())
	if err != nil {
		return ListLogsResponse{}, err
	}

	rows, err := app.NewEventService(s.repo).ListEvents(ctx, project, app.ListEventsParams{
		Categories: categories,
		Since:      since,
		Limit:      input.Limit,
		Order:      input.Order,
	})
	if err != nil {
		return ListLogsResponse{}, err
	}

	resolvedOrder := strings.ToLower(strings.TrimSpace(input.Order))
	if resolvedOrder != "asc" && resolvedOrder != "desc" {
		resolvedOrder = "desc"
	}

	resp := ListLogsResponse{
		Project: projectSummary(project),
		Rows:    logsRows(rows),
		Order:   resolvedOrder,
	}
	if !since.IsZero() {
		resp.WindowSince = since.UTC().Format(logsSinceLayout)
	}
	return resp, nil
}

// normalizeLogsCategories resolves the agent-supplied category strings
// into the domain enum. Empty / missing input means "no filter" so the
// SQL layer reads every category. Unknown values are rejected up front
// with a self-describing validation error — silent drop would mask
// typos that produce a misleading empty response.
func normalizeLogsCategories(raw []string) ([]domain.EventCategory, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	known := map[string]domain.EventCategory{}
	for _, c := range domain.KnownEventCategories {
		known[string(c)] = c
	}
	var out []domain.EventCategory
	seen := map[domain.EventCategory]struct{}{}
	for _, r := range raw {
		key := strings.TrimSpace(r)
		if key == "" {
			continue
		}
		cat, ok := known[key]
		if !ok {
			return nil, domain.NewError(domain.ErrValidation,
				fmt.Sprintf("unknown logs category %q; allowed: %s", r, logsKnownCategoryList()),
				map[string]any{"category": r, "allowed": logsKnownCategoryList()})
		}
		if _, dup := seen[cat]; dup {
			continue
		}
		seen[cat] = struct{}{}
		out = append(out, cat)
	}
	return out, nil
}

// resolveLogsSince converts the `since` input into a concrete time
// floor. Empty input falls back to the project's configured Logs
// window (Snapshot.LogsWindowDays). Non-empty input is parsed first
// via time.ParseDuration (standard Go shorthand: "24h", "30m") and
// then via the day-aware fallback ("7d", "30d") so callers can
// express both granularities. Anything that fails both parses
// surfaces a validation error rather than a silent zero-floor.
//
// The `now` callback is injected so tests can substitute a
// deterministic clock (internal/testfakes/clock.Fake.Now) instead of
// snapshotting time.Now() and comparing with a tolerance window.
// Production callers route through Service.nowFunc(), which defaults
// to time.Now — the public ListLogs behaviour is unchanged.
func resolveLogsSince(raw string, snap *config.Snapshot, now func() time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if snap == nil {
			return time.Time{}, nil
		}
		window := snap.LogsWindowDays()
		if window <= 0 {
			return time.Time{}, nil
		}
		return now().Add(-window), nil
	}
	if d, err := time.ParseDuration(trimmed); err == nil {
		if d <= 0 {
			return time.Time{}, domain.NewError(domain.ErrValidation,
				"logs.since duration must be positive (got "+trimmed+")",
				map[string]any{"since": trimmed})
		}
		return now().Add(-d), nil
	}
	if d, ok := parseDayDuration(trimmed); ok {
		if d <= 0 {
			return time.Time{}, domain.NewError(domain.ErrValidation,
				"logs.since duration must be positive (got "+trimmed+")",
				map[string]any{"since": trimmed})
		}
		return now().Add(-d), nil
	}
	return time.Time{}, domain.NewError(domain.ErrValidation,
		"logs.since must be a Go duration (e.g. \"24h\", \"30m\") or N-day shorthand (e.g. \"7d\"); got "+trimmed,
		map[string]any{"since": trimmed})
}

// parseDayDuration recognises the day-suffixed shorthand
// `time.ParseDuration` does not natively support (Go's
// ParseDuration stops at hours). Accepts integer days only —
// fractional days fall back to the standard parser ("12h" etc.).
func parseDayDuration(raw string) (time.Duration, bool) {
	if !strings.HasSuffix(raw, "d") {
		return 0, false
	}
	digits := strings.TrimSuffix(raw, "d")
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return time.Duration(n) * 24 * time.Hour, true
}

// logsKnownCategoryList returns the sorted comma-separated list of
// EventCategory string values agents may pass. Surfaced inside
// validation errors so the rejection self-describes the legal set.
func logsKnownCategoryList() string {
	names := make([]string, len(domain.KnownEventCategories))
	for i, c := range domain.KnownEventCategories {
		names[i] = string(c)
	}
	return strings.Join(names, ", ")
}

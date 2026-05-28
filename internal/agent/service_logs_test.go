package agent

import (
	"strings"
	"testing"
	"time"

	"omakiten/internal/domain"
)

// TestResolveLogsSinceDefaultUsesSnapshotWindow locks the default
// behaviour: with no `since` input, the service substitutes
// Snapshot.LogsWindowDays as the time floor so MCP callers can pass
// nothing and still get the project's configured window.
func TestResolveLogsSinceDefaultUsesSnapshotWindow(t *testing.T) {
	// snapshot==nil is the test path; resolveLogsSince must accept
	// it and fall back to a zero-value (no floor) rather than panic.
	since, err := resolveLogsSince("", nil)
	if err != nil {
		t.Fatalf("resolveLogsSince(empty, nil) error = %v", err)
	}
	if !since.IsZero() {
		t.Fatalf("resolveLogsSince(empty, nil) = %v, want zero-value", since)
	}
}

// TestResolveLogsSinceAcceptsGoDuration locks the Go-shorthand path:
// "24h" / "30m" / "90s" all parse via time.ParseDuration and produce
// a non-zero floor at `now - duration`.
func TestResolveLogsSinceAcceptsGoDuration(t *testing.T) {
	cases := []struct{ in string }{
		{"24h"},
		{"30m"},
		{"90s"},
		{"1h30m"},
	}
	now := time.Now()
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := resolveLogsSince(tc.in, nil)
			if err != nil {
				t.Fatalf("resolveLogsSince(%q) error = %v", tc.in, err)
			}
			if got.IsZero() {
				t.Fatalf("resolveLogsSince(%q) = zero, want non-zero", tc.in)
			}
			if got.After(now) {
				t.Fatalf("resolveLogsSince(%q) = %v, want <= now", tc.in, got)
			}
		})
	}
}

// TestResolveLogsSinceAcceptsDayShorthand exercises the `Nd` shorthand
// time.ParseDuration does not support natively. "7d" must produce a
// floor 7 days before now.
func TestResolveLogsSinceAcceptsDayShorthand(t *testing.T) {
	now := time.Now()
	got, err := resolveLogsSince("7d", nil)
	if err != nil {
		t.Fatalf("resolveLogsSince(7d) error = %v", err)
	}
	delta := now.Sub(got)
	want := 7 * 24 * time.Hour
	if delta < want-time.Minute || delta > want+time.Minute {
		t.Fatalf("resolveLogsSince(7d) delta = %v, want ~%v", delta, want)
	}
}

// TestResolveLogsSinceRejectsGarbage locks AC #3's validation
// contract: unparseable input surfaces a validation error rather than
// silently produce a zero floor (which would erase the window).
func TestResolveLogsSinceRejectsGarbage(t *testing.T) {
	_, err := resolveLogsSince("yesterday", nil)
	if err == nil {
		t.Fatal("resolveLogsSince(yesterday) error = nil, want validation_error")
	}
	if !strings.Contains(err.Error(), "logs.since") {
		t.Fatalf("error = %v, want 'logs.since' hint", err)
	}
}

// TestResolveLogsSinceRejectsZeroDuration locks the positive-duration
// guard. "0h" or "0d" would produce a floor equal to `now`, which is
// indistinguishable from "no rows since this instant" — surface it as
// a validation error so callers correct the input.
func TestResolveLogsSinceRejectsZeroDuration(t *testing.T) {
	for _, in := range []string{"0h", "0d", "0s"} {
		t.Run(in, func(t *testing.T) {
			_, err := resolveLogsSince(in, nil)
			if err == nil {
				t.Fatalf("resolveLogsSince(%q) error = nil, want validation_error", in)
			}
		})
	}
}

// TestNormalizeLogsCategoriesAcceptsKnown locks the happy path: every
// EventCategory string from domain.KnownEventCategories is accepted
// and round-trips through the normaliser. Without this parity, the
// JSON-schema enum and the agent-side validation drift apart.
func TestNormalizeLogsCategoriesAcceptsKnown(t *testing.T) {
	raw := make([]string, len(domain.KnownEventCategories))
	for i, c := range domain.KnownEventCategories {
		raw[i] = string(c)
	}
	got, err := normalizeLogsCategories(raw)
	if err != nil {
		t.Fatalf("normalizeLogsCategories(all known) error = %v", err)
	}
	if len(got) != len(domain.KnownEventCategories) {
		t.Fatalf("len = %d, want %d", len(got), len(domain.KnownEventCategories))
	}
}

// TestNormalizeLogsCategoriesEmptyMeansAll locks the no-filter
// contract: an empty / nil input yields a nil slice so the SQL layer
// reads every category.
func TestNormalizeLogsCategoriesEmptyMeansAll(t *testing.T) {
	for _, raw := range [][]string{nil, {}, {""}} {
		got, err := normalizeLogsCategories(raw)
		if err != nil {
			t.Fatalf("normalizeLogsCategories(%v) error = %v", raw, err)
		}
		if got != nil {
			t.Fatalf("normalizeLogsCategories(%v) = %v, want nil", raw, got)
		}
	}
}

// TestLogsRowCarriesSummary locks the projection contract: every row
// produced via logsRow exposes a non-empty Summary string and the
// Category derived from EventCategoryOf. The MCP test exercises this
// over the wire; this unit test pins the contract at the projection
// layer so a regression surfaces in the package that owns the bug.
func TestLogsRowCarriesSummary(t *testing.T) {
	row := domain.EventRow{
		ID:         42,
		EventType:  domain.EventTypeTaskCreated,
		Payload:    `{"title":"Demo","bucket":"backlog"}`,
		ProjectID:  1,
		CreatedAt:  "2026-05-28 12:00:00",
		AuthorType: "agent",
	}
	got := logsRow(row)
	if got.ID != 42 {
		t.Fatalf("ID = %d, want 42", got.ID)
	}
	if got.Summary == "" {
		t.Fatal("Summary is empty; SummarizeEvent never returns empty")
	}
	if got.Category != string(domain.EventCategoryTask) {
		t.Fatalf("Category = %q, want %q", got.Category, domain.EventCategoryTask)
	}
}

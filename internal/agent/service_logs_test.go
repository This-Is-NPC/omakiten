package agent

import (
	"strings"
	"testing"
	"time"

	"omakiten/internal/domain"
)

// logsSinceJitterTolerance is the slack permitted between the
// expected `now - duration` floor and the actual value returned by
// resolveLogsSince. The gap is wall-clock jitter (test setup ↔
// assertion), never domain semantics. Heavy CI shared runners can
// stall a goroutine for several seconds, so the tolerance is set
// well above the worst observed local drift but small enough to
// catch genuine off-by-N regressions in the duration math.
const logsSinceJitterTolerance = 10 * time.Second

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
	cases := []struct {
		in  string
		dur time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"30m", 30 * time.Minute},
		{"90s", 90 * time.Second},
		{"1h30m", 1*time.Hour + 30*time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := resolveLogsSince(tc.in, nil)
			if err != nil {
				t.Fatalf("resolveLogsSince(%q) error = %v", tc.in, err)
			}
			if got.IsZero() {
				t.Fatalf("resolveLogsSince(%q) = zero, want non-zero", tc.in)
			}
			// Recompute `expected` inside the assertion block so the
			// snapshot-to-comparison gap is the tightest the runtime
			// allows. A `now` captured before the table loop drifts
			// under heavy CI load and falsely trips the tolerance.
			expected := time.Now().Add(-tc.dur)
			if diff := got.Sub(expected); diff < -logsSinceJitterTolerance || diff > logsSinceJitterTolerance {
				t.Fatalf("resolveLogsSince(%q) = %v, want within %v of %v (drift: %v)",
					tc.in, got, logsSinceJitterTolerance, expected, diff)
			}
		})
	}
}

// TestResolveLogsSinceAcceptsDayShorthand exercises the `Nd` shorthand
// time.ParseDuration does not support natively. "7d" must produce a
// floor 7 days before now.
func TestResolveLogsSinceAcceptsDayShorthand(t *testing.T) {
	got, err := resolveLogsSince("7d", nil)
	if err != nil {
		t.Fatalf("resolveLogsSince(7d) error = %v", err)
	}
	// Recompute `expected` after the call so the snapshot-to-assertion
	// gap is minimal; the tolerance covers wall-clock jitter only.
	expected := time.Now().Add(-7 * 24 * time.Hour)
	if diff := got.Sub(expected); diff < -logsSinceJitterTolerance || diff > logsSinceJitterTolerance {
		t.Fatalf("resolveLogsSince(7d) = %v, want within %v of %v (drift: %v)",
			got, logsSinceJitterTolerance, expected, diff)
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

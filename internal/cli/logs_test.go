package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"omakiten/internal/domain"
	"omakiten/internal/testfakes/clock"
	"omakiten/internal/testutil"
)

// cliFakeClockAnchor is the deterministic instant the clock-dependent
// resolveLogSince tests seed their fake clock at. Picked arbitrarily
// — the value matters only because the assertions compare exact
// equality against `anchor.UTC() - duration`, retiring the
// wall-clock jitter tolerance the legacy tests carried.
var cliFakeClockAnchor = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// TestParseLogCategories pins the flag→filter mapping for every
// supported shape: empty input, `all` short-circuit, repeatable
// + comma-separated entries, dedup, unknown rejection. Keeping this
// in pure-table form means the cobra wiring stays trivial and the
// failure messages point at the literal token that broke.
func TestParseLogCategories(t *testing.T) {
	allowedKnown := append([]string{"all"}, knownCategoryNamesForSort()...)
	sort.Strings(allowedKnown)

	cases := []struct {
		name    string
		input   []string
		want    []domain.EventCategory
		wantErr string
	}{
		{name: "empty", input: nil, want: nil},
		{name: "explicit all", input: []string{"all"}, want: nil},
		{name: "single category", input: []string{"task"}, want: []domain.EventCategory{domain.EventCategoryTask}},
		{
			name:  "repeatable AND comma separated",
			input: []string{"task", "plan,hook"},
			want:  []domain.EventCategory{domain.EventCategoryTask, domain.EventCategoryPlan, domain.EventCategoryHook},
		},
		{
			name:  "dedup keeps first occurrence order",
			input: []string{"task", "task,plan"},
			want:  []domain.EventCategory{domain.EventCategoryTask, domain.EventCategoryPlan},
		},
		{
			name:  "trims and lowercases",
			input: []string{" TASK ", "Plan"},
			want:  []domain.EventCategory{domain.EventCategoryTask, domain.EventCategoryPlan},
		},
		{
			name:  "all short-circuits even when mixed",
			input: []string{"task", "all", "plan"},
			want:  nil,
		},
		{
			name:    "unknown category",
			input:   []string{"made-up"},
			wantErr: "made-up",
		},
		{
			name:  "empty token is skipped",
			input: []string{"task,,plan"},
			want:  []domain.EventCategory{domain.EventCategoryTask, domain.EventCategoryPlan},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertParseLogCategories(t, tc.input, tc.want, tc.wantErr)
		})
	}
}

func assertParseLogCategories(t *testing.T, input []string, want []domain.EventCategory, wantErr string) {
	t.Helper()
	got, err := parseLogCategories(input)
	if wantErr != "" {
		if err == nil {
			t.Fatalf("parseLogCategories(%v) error = nil, want failure containing %q", input, wantErr)
		}
		var coded *domain.CodedError
		if !errorsAs(err, &coded) {
			t.Fatalf("parseLogCategories(%v) error = %T, want CodedError", input, err)
		}
		if coded.Code != domain.ErrValidation {
			t.Fatalf("parseLogCategories(%v) code = %s, want %s", input, coded.Code, domain.ErrValidation)
		}
		return
	}
	if err != nil {
		t.Fatalf("parseLogCategories(%v) error = %v", input, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLogCategories(%v) = %v, want %v", input, got, want)
	}
}

// TestResolveLogSinceFlagWins exercises the --since flag against a
// fake snapshot: explicit flag wins over the snapshot default, plain
// `d`-suffixed durations parse, and an invalid string surfaces a
// typed validation error. The fake clock pins `now` to
// cliFakeClockAnchor so each assertion is exact equality against
// `anchor.UTC() - dur` — no wall-clock tolerance required.
func TestResolveLogSinceFlagWins(t *testing.T) {
	t.Parallel()
	anchorUTC := cliFakeClockAnchor.UTC()
	cases := []struct {
		name, flag       string
		window, duration time.Duration
		invalid          bool
	}{
		{name: "flag overrides snapshot window", flag: "24h", duration: 24 * time.Hour, window: 30 * 24 * time.Hour},
		{name: "days suffix accepted", flag: "7d", duration: 7 * 24 * time.Hour, window: 30 * 24 * time.Hour},
		{name: "falls back to snapshot window when flag empty", duration: 30 * 24 * time.Hour, window: 30 * 24 * time.Hour},
		{name: "invalid duration returns coded error", flag: "not-a-duration", invalid: true, window: 30 * 24 * time.Hour},
		{name: "zero window returns zero time floor", window: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertResolveLogSince(t, tc.flag, stubLogsSnapshot{window: tc.window}, tc.duration, tc.invalid, anchorUTC)
		})
	}
}

func assertResolveLogSince(t *testing.T, flag string, snap stubLogsSnapshot, duration time.Duration, invalid bool, anchor time.Time) {
	t.Helper()
	fake := clock.New(cliFakeClockAnchor)
	got, err := resolveLogSince(flag, snap, fake.Now)
	if invalid {
		var coded *domain.CodedError
		if err == nil || !errorsAs(err, &coded) || coded.Code != domain.ErrValidation {
			t.Fatalf("resolveLogSince(%s) error = %v, want validation error", flag, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("resolveLogSince(%s) error = %v", flag, err)
	}
	if duration == 0 {
		if !got.IsZero() {
			t.Fatalf("resolveLogSince(empty, zero snap) = %v, want zero time", got)
		}
		return
	}
	want := anchor.Add(-duration)
	if !got.Equal(want) {
		t.Fatalf("resolveLogSince(%s) = %v, want %v", flag, got, want)
	}
}

// TestProjectLogRowsCarriesSummary asserts the JSON projection's
// `summary` field is the SummarizeEvent output verbatim — AC #6.
// Picks two divergent event types (comment + tool call) so any drift
// between SummarizeEvent and the projection helper trips the test.
func TestProjectLogRowsCarriesSummary(t *testing.T) {
	rows := []domain.EventRow{
		{
			EventType:  domain.EventTypeComment,
			Body:       "remember this",
			AuthorType: "human",
			CreatedAt:  "2026-05-27 10:00:00",
			EntityType: "task",
			EntityID:   42,
		},
		{
			EventType:  domain.EventTypeCLIToolCall,
			Payload:    `{"tool_name":"tasks.create","status":"ok","duration_ms":12}`,
			Source:     "cli",
			Status:     "ok",
			DurationMs: 12,
			CreatedAt:  "2026-05-27 10:00:01",
		},
	}

	for i := range rows {
		rows[i] = testutil.EventRegistry().Prepare(rows[i])
	}
	got := projectLogRows(rows)
	if len(got) != 2 {
		t.Fatalf("projectLogRows() len = %d, want 2", len(got))
	}
	for i, row := range got {
		want := testutil.EventRegistry().Summarize(rows[i])
		if row.Summary != want {
			t.Fatalf("projectLogRows()[%d].Summary = %q, want %q", i, row.Summary, want)
		}
	}
	if got[0].Category != string(domain.EventCategoryComment) {
		t.Fatalf("comment row category = %s, want %s", got[0].Category, domain.EventCategoryComment)
	}
	if got[1].Category != string(domain.EventCategoryToolCall) {
		t.Fatalf("tool-call row category = %s, want %s", got[1].Category, domain.EventCategoryToolCall)
	}
}

// TestCLILogsRunsAndShapeMatchesAC drives the full cobra tree: seed
// a project + a task + a comment through the normal CLI surface
// (each of those writes an event row), then assert `okt logs`
// emits the new 5-field shape with category + summary populated.
func TestCLILogsRunsAndShapeMatchesAC(t *testing.T) {
	// Slow integration test: drives the cobra tree through init/add/
	// comment/logs against a real on-disk SQLite database. Opt out of
	// `go test -short` so quick smoke runs stay snappy; the unit-level
	// projection and parse tests above still cover the shape contract.
	//
	// t.Parallel is intentionally NOT called: this test invokes
	// t.Chdir, which mutates the process working directory and is
	// documented as incompatible with parallel tests.
	if testing.Short() {
		t.Skip("integration test: skipped under -short")
	}
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "task", "create", "--confirm", "-t", "First")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "remember this")

	assertCLILogsDefault(t, dbPath, configPath)
	assertCLILogsFilters(t, dbPath, configPath)
	assertCLILogsLimitAndSince(t, dbPath, configPath)
	assertCLILogsInvalidInputs(t, dbPath, configPath)
}

func assertCLILogsDefault(t *testing.T, dbPath, configPath string) {
	t.Helper()
	events := decodeLogEvents(t, runCLI(t, dbPath, configPath, "logs"))
	if len(events) == 0 {
		t.Fatal("okt logs returned zero events")
	}
	for _, ev := range events {
		for _, key := range []string{"summary", "category", "time", "event_type"} {
			if ev[key] == nil || ev[key] == "" {
				t.Fatalf("event missing %s: %v", key, ev)
			}
		}
	}
}

func assertCLILogsFilters(t *testing.T, dbPath, configPath string) {
	t.Helper()
	events := decodeLogEvents(t, runCLI(t, dbPath, configPath, "logs", "--category", "comment"))
	if len(events) == 0 {
		t.Fatal("comment category returned zero events")
	}
	for _, ev := range events {
		if ev["category"] != string(domain.EventCategoryComment) {
			t.Fatalf("category filter leaked %v: %v", ev["category"], ev)
		}
	}
	events = decodeLogEvents(t, runCLI(t, dbPath, configPath, "logs", "--category", "task", "--category", "comment"))
	allowed := map[string]bool{string(domain.EventCategoryTask): true, string(domain.EventCategoryComment): true}
	for _, ev := range events {
		cat, _ := ev["category"].(string)
		if !allowed[cat] {
			t.Fatalf("category union leaked %q: %v", cat, ev)
		}
	}
	events = decodeLogEvents(t, runCLI(t, dbPath, configPath, "logs", "--category", "task,comment"))
	if len(events) == 0 {
		t.Fatal("comma-separated category returned zero events")
	}
}

func assertCLILogsLimitAndSince(t *testing.T, dbPath, configPath string) {
	t.Helper()
	events := decodeLogEvents(t, runCLI(t, dbPath, configPath, "logs", "--limit", "1"))
	if len(events) != 1 {
		t.Fatalf("okt logs --limit 1 returned %d events, want 1", len(events))
	}
	// The 24h window includes the rows written above.
	events = decodeLogEvents(t, runCLI(t, dbPath, configPath, "logs", "--since", "24h"))
	if len(events) == 0 {
		t.Fatal("okt logs --since 24h returned zero events")
	}
}

func assertCLILogsInvalidInputs(t *testing.T, dbPath, configPath string) {
	t.Helper()
	runCLIExpectError(t, dbPath, configPath, "validation_error", "logs", "--category", "made-up")
	runCLIExpectError(t, dbPath, configPath, "validation_error", "logs", "--since", "not-a-duration")
}

// stubLogsSnapshot is a small shim that fulfils the interface
// resolveLogSince expects without dragging *config.Snapshot
// construction into a unit test. Keeps the duration-parsing branch
// fully exercised without going through bundle loading.
type stubLogsSnapshot struct {
	window time.Duration
}

func (s stubLogsSnapshot) LogsWindowDays() time.Duration { return s.window }

// errorsAs is a thin shim around errors.As that avoids importing the
// stdlib in test-only code paths where the import set is already
// stretched. Keeps the test file self-contained.
func errorsAs(err error, target **domain.CodedError) bool {
	for cur := err; cur != nil; {
		if coded, ok := cur.(*domain.CodedError); ok {
			*target = coded
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := cur.(unwrapper)
		if !ok {
			return false
		}
		cur = u.Unwrap()
	}
	return false
}

// decodeLogEvents pulls the events array out of the runCLI JSON
// envelope so per-case assertions can iterate the rows without
// repeating the json.Unmarshal scaffolding.
func decodeLogEvents(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; raw = %s", err, raw)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope missing data: %s", raw)
	}
	rawEvents, ok := data["events"].([]any)
	if !ok {
		t.Fatalf("envelope missing events array: %s", raw)
	}
	out := make([]map[string]any, 0, len(rawEvents))
	for _, ev := range rawEvents {
		m, ok := ev.(map[string]any)
		if !ok {
			t.Fatalf("event is not an object: %v", ev)
		}
		out = append(out, m)
	}
	return out
}

// knownCategoryNamesForSort returns a copy of the canonical names so
// the parse-categories table can sort its expected `allowed` slice
// without mutating the package-level constant.
func knownCategoryNamesForSort() []string {
	out := make([]string, 0, len(domain.KnownEventCategories))
	for _, c := range domain.KnownEventCategories {
		out = append(out, string(c))
	}
	return out
}

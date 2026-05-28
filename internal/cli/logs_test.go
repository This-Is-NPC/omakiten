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
)

// cliJitterTolerance is the wall-clock slack permitted between the
// expected `now - duration` floor and the value resolveLogSince
// returns. The gap is process-scheduling jitter, not domain
// semantics; 5s is well above the worst observed local drift while
// still tight enough to catch off-by-N regressions in the math.
const cliJitterTolerance = 5 * time.Second

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
			got, err := parseLogCategories(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseLogCategories(%v) error = nil, want failure containing %q", tc.input, tc.wantErr)
				}
				var coded *domain.CodedError
				if !errorsAs(err, &coded) {
					t.Fatalf("parseLogCategories(%v) error = %T, want CodedError", tc.input, err)
				}
				if coded.Code != domain.ErrValidation {
					t.Fatalf("parseLogCategories(%v) code = %s, want %s", tc.input, coded.Code, domain.ErrValidation)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLogCategories(%v) error = %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseLogCategories(%v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestResolveLogSinceFlagWins exercises the --since flag against a
// fake snapshot: explicit flag wins over the snapshot default, plain
// `d`-suffixed durations parse, and an invalid string surfaces a
// typed validation error.
func TestResolveLogSinceFlagWins(t *testing.T) {
	snap := stubLogsSnapshot{window: 30 * 24 * time.Hour}

	t.Run("flag overrides snapshot window", func(t *testing.T) {
		got, err := resolveLogSince("24h", snap)
		if err != nil {
			t.Fatalf("resolveLogSince() error = %v", err)
		}
		want := time.Now().UTC().Add(-24 * time.Hour)
		if diff := absDuration(got.Sub(want)); diff > cliJitterTolerance {
			t.Fatalf("resolveLogSince(24h) = %v, want ~%v (diff %v)", got, want, diff)
		}
	})

	t.Run("days suffix accepted", func(t *testing.T) {
		got, err := resolveLogSince("7d", snap)
		if err != nil {
			t.Fatalf("resolveLogSince(7d) error = %v", err)
		}
		want := time.Now().UTC().Add(-7 * 24 * time.Hour)
		if diff := absDuration(got.Sub(want)); diff > cliJitterTolerance {
			t.Fatalf("resolveLogSince(7d) = %v, want ~%v (diff %v)", got, want, diff)
		}
	})

	t.Run("falls back to snapshot window when flag empty", func(t *testing.T) {
		got, err := resolveLogSince("", snap)
		if err != nil {
			t.Fatalf("resolveLogSince() error = %v", err)
		}
		want := time.Now().UTC().Add(-30 * 24 * time.Hour)
		if diff := absDuration(got.Sub(want)); diff > cliJitterTolerance {
			t.Fatalf("resolveLogSince() = %v, want ~%v (diff %v)", got, want, diff)
		}
	})

	t.Run("invalid duration returns coded error", func(t *testing.T) {
		_, err := resolveLogSince("not-a-duration", snap)
		if err == nil {
			t.Fatalf("resolveLogSince(not-a-duration) error = nil, want failure")
		}
		var coded *domain.CodedError
		if !errorsAs(err, &coded) {
			t.Fatalf("resolveLogSince(not-a-duration) error = %T, want CodedError", err)
		}
		if coded.Code != domain.ErrValidation {
			t.Fatalf("resolveLogSince(not-a-duration) code = %s, want %s", coded.Code, domain.ErrValidation)
		}
	})

	t.Run("zero window returns zero time floor", func(t *testing.T) {
		got, err := resolveLogSince("", stubLogsSnapshot{window: 0})
		if err != nil {
			t.Fatalf("resolveLogSince() error = %v", err)
		}
		if !got.IsZero() {
			t.Fatalf("resolveLogSince(empty, zero snap) = %v, want zero time", got)
		}
	})
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

	got := projectLogRows(rows)
	if len(got) != 2 {
		t.Fatalf("projectLogRows() len = %d, want 2", len(got))
	}
	for i, row := range got {
		want := domain.SummarizeEvent(rows[i])
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
	runCLI(t, dbPath, configPath, "add", "-t", "First")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "remember this")

	t.Run("default scope returns events", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "logs")
		events := decodeLogEvents(t, out)
		if len(events) == 0 {
			t.Fatalf("okt logs returned zero events; output = %s", out)
		}
		for _, ev := range events {
			if ev["summary"] == nil || ev["summary"] == "" {
				t.Fatalf("event missing summary: %v", ev)
			}
			if ev["category"] == nil || ev["category"] == "" {
				t.Fatalf("event missing category: %v", ev)
			}
			if ev["time"] == nil || ev["time"] == "" {
				t.Fatalf("event missing time: %v", ev)
			}
			if ev["event_type"] == nil || ev["event_type"] == "" {
				t.Fatalf("event missing event_type: %v", ev)
			}
		}
	})

	t.Run("category filter narrows results", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "logs", "--category", "comment")
		events := decodeLogEvents(t, out)
		if len(events) == 0 {
			t.Fatalf("okt logs --category comment returned zero events; output = %s", out)
		}
		for _, ev := range events {
			if ev["category"] != string(domain.EventCategoryComment) {
				t.Fatalf("category filter leaked %v: %v", ev["category"], ev)
			}
		}
	})

	t.Run("repeatable category flag ANDs categories", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "logs", "--category", "task", "--category", "comment")
		events := decodeLogEvents(t, out)
		if len(events) == 0 {
			t.Fatalf("okt logs --category task --category comment returned zero events; output = %s", out)
		}
		allowed := map[string]bool{
			string(domain.EventCategoryTask):    true,
			string(domain.EventCategoryComment): true,
		}
		for _, ev := range events {
			cat, _ := ev["category"].(string)
			if !allowed[cat] {
				t.Fatalf("category union leaked %q: %v", cat, ev)
			}
		}
	})

	t.Run("comma-separated category form parses", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "logs", "--category", "task,comment")
		events := decodeLogEvents(t, out)
		if len(events) == 0 {
			t.Fatalf("okt logs --category task,comment returned zero events; output = %s", out)
		}
	})

	t.Run("limit caps rows", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "logs", "--limit", "1")
		events := decodeLogEvents(t, out)
		if len(events) != 1 {
			t.Fatalf("okt logs --limit 1 returned %d events, want 1", len(events))
		}
	})

	t.Run("since shorthand parses", func(t *testing.T) {
		// 24h window will still include the rows we just wrote.
		out := runCLI(t, dbPath, configPath, "logs", "--since", "24h")
		events := decodeLogEvents(t, out)
		if len(events) == 0 {
			t.Fatalf("okt logs --since 24h returned zero events; output = %s", out)
		}
	})

	t.Run("invalid category surfaces validation_error", func(t *testing.T) {
		runCLIExpectError(t, dbPath, configPath, "validation_error",
			"logs", "--category", "made-up")
	})

	t.Run("invalid since surfaces validation_error", func(t *testing.T) {
		runCLIExpectError(t, dbPath, configPath, "validation_error",
			"logs", "--since", "not-a-duration")
	})
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

// absDuration returns |d|; used to give the time-based tests a small
// tolerance window so we don't race against the wall clock between
// the test setup and the comparison call.
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

package logs

import (
	"fmt"
	"sync"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

var (
	logsEventRegistryOnce sync.Once
	logsEventRegistryErr  error
	logsFixtureRegistry   *domain.EventRegistry
)

func logsEnsureEventRegistry() {
	logsEventRegistryOnce.Do(func() {
		cfg, err := config.LoadKitConfigByKey("omakase")
		if err != nil {
			logsEventRegistryErr = fmt.Errorf("load omakase kit: %w", err)
			return
		}
		var registryErr error
		logsFixtureRegistry, registryErr = config.BuildEventRegistry(cfg.Events)
		if err := registryErr; err != nil {
			logsEventRegistryErr = fmt.Errorf("hydrate event registry: %w", err)
		}
	})
	if logsEventRegistryErr != nil {
		panic(logsEventRegistryErr)
	}
}

// logsGoldenEpoch is the instant every fixture row is stamped from. It is a
// literal, never time.Now(): the panel prints ShortTime(row.CreatedAt) verbatim
// and a clock-derived stamp would rewrite every fixture on every run — the one
// determinism leak screentest.Record's double-paint cannot catch, because two
// paintings taken microseconds apart agree.
var logsGoldenEpoch = time.Date(2026, time.May, 27, 13, 45, 8, 0, time.UTC)

// logsGoldenStamp renders the SQLite "YYYY-MM-DD HH:MM:SS" stamp for row `i`,
// walking backwards from the epoch so the feed reads newest-first the way the
// repository's default descending order delivers it.
func logsGoldenStamp(i int) string {
	return logsGoldenEpoch.Add(-time.Duration(i) * 97 * time.Second).Format("2006-01-02 15:04:05")
}

// Long fixture values, sized against the DETAIL budget the wide grid computes:
// 130 columns at 200, 50 at 120, and roughly 48 inside the compact row at 80.
// Each of these produces a detail string that fits whole at 200 and truncates
// at the two narrower widths, so the truncation column is part of the record.
const (
	logsGoldenLongTitle   = "Record three-width characterization goldens for the Stats Logs event inspector before the migration"
	logsGoldenLongTool    = "omakiten.tasks_create_intent_with_a_deliberately_long_operation_name"
	logsGoldenLongHint    = "attach the test command, an output snippet and a wall-clock duration before leaving development"
	logsGoldenLongComment = "the inspector has to survive a comment body long enough that the DETAIL column truncates at every width but the widest"
)

// logsGoldenTemplates is one of every card shape the renderer can draw: a row
// for each domain.KnownEventCategories (so every CategoryAccent arm is exercised)
// plus an unregistered event_type, which is the arm that falls back to the raw
// key for its TYPE label and to the neutral hint style for its colour.
//
// The ENTITY and WHO columns are deliberately uneven — a 21-character
// `orchestration_plan#12` against a 16-wide column, a 12-character author type
// against an 8-wide one — so the fixed-width truncation of both is recorded
// alongside the DETAIL truncation.
func logsGoldenTemplates() []domain.EventRow {
	return []domain.EventRow{
		{
			EntityType: "task", EntityID: 2421,
			EventType: domain.EventTypeTaskCreated,
			Source:    "mcp",
			Payload:   fmt.Sprintf(`{"title":%q,"bucket":"dev","priority":"high"}`, logsGoldenLongTitle),
		},
		{
			EntityType: "task", EntityID: 118,
			EventType: domain.EventTypeTaskMoved,
			Source:    "cli",
			Payload:   `{"from":"backlog","to":"dev"}`,
		},
		{
			EntityType: "task", EntityID: 2421,
			EventType:  domain.EventTypeComment,
			AuthorType: "collaborator",
			Body:       logsGoldenLongComment,
		},
		{
			EntityType: "task", EntityID: 2420,
			EventType:  domain.EventTypeCommentEdited,
			AuthorType: "agent",
			Payload:    `{"comment_id":874,"pinned":{"from":false,"to":true}}`,
		},
		{
			EntityType: "orchestration_plan", EntityID: 12,
			EventType: domain.EventTypePlanCreated,
			Source:    "cli",
			Payload:   `{"slug":"screen-goldens","name":"Characterization baseline for the extracted screens"}`,
		},
		{
			EntityType: "orchestration_plan", EntityID: 12,
			EventType: domain.EventTypePlanWaveAdded,
			Source:    "cli",
			Payload:   `{"position":3,"name":"record the remaining screen packages"}`,
		},
		{
			EntityType: "task", EntityID: 2421,
			EventType: domain.EventTypeTagAdded,
			Source:    "mcp",
			Payload:   `{"tag_name":"characterization","entity_type":"task"}`,
		},
		{
			EntityType: "task", EntityID: 2421,
			EventType: domain.EventTypeDependencyAdded,
			Source:    "mcp",
			Payload:   `{"depends_on_task_id":2419}`,
		},
		{
			EntityType: "task", EntityID: 2419,
			EventType: domain.EventTypeGuardViolated,
			Source:    "cli",
			Payload:   fmt.Sprintf(`{"operation":"task.move","rule":"comments_tagged","hint":%q}`, logsGoldenLongHint),
		},
		{
			EntityType: "error", EntityID: 41,
			EventType: domain.EventTypeErrorRecorded,
			Source:    "mcp",
			Payload:   `{"tags":["golden","determinism"],"has_context":true}`,
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeHookExecuted,
			Source:    "mcp",
			Status:    "ok",
			Payload:   `{"action":"shell","event_type":"task.created","success":true}`,
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeCLIToolCall,
			Source:    "cli", Status: "ok", DurationMs: 12,
			Payload: `{"tool_name":"app.TaskService.Add","source":"cli","status":"ok","duration_ms":12}`,
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeMCPToolCall,
			Source:    "mcp", Status: "error", DurationMs: 1843,
			ErrorMessage: "guard refused the transition",
			Payload:      fmt.Sprintf(`{"tool_name":%q,"source":"mcp","status":"error","duration_ms":1843}`, logsGoldenLongTool),
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeTUIToolCall,
			Source:    "tui", Status: "running",
			Payload: `{"tool_name":"app.BoardService.Reload","source":"tui","status":"running"}`,
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeTrickExecuted,
			Source:    "tui",
			Payload:   `{"verb":"move","operand":"2421 review","raw":"move 2421 review"}`,
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeBundleSwapped,
			Source:    "cli",
			Payload:   `{"from_workflow":"omakase","to_workflow":"lean","orphan_count":4}`,
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeUpdateSwapCompleted,
			Source:    "cli",
			Payload:   `{"from_version":"0.14.2","to_version":"0.15.0"}`,
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeTUIHealthCheckFailed,
			Source:    "tui",
			Payload:   `{"validator_first_error_kind":"workflow.bucket.missing"}`,
		},
		{
			// Unregistered on purpose: EventCategoryOf returns unknown, so the
			// TYPE cell falls back to the raw key, CategoryAccent falls back to
			// the neutral hint tone, and SummarizeEvent falls back to the
			// condensed-JSON payload. json.Marshal sorts object keys, so the
			// condensed form is stable across runs.
			EntityType: "system", EntityID: 0,
			EventType: "__fixture.unregistered",
			Payload:   `{"note":"an event_type the registry has never seen","seq":7}`,
		},
	}
}

// logsGoldenFeed repeats the template deck `cycles` times and stamps every row
// with a distinct id and a distinct literal timestamp. It is called afresh on
// every Build and every Bind, so no two materialisations share a slice.
func logsGoldenFeed(cycles int) []domain.EventRow {
	logsEnsureEventRegistry()
	templates := logsGoldenTemplates()
	rows := make([]domain.EventRow, 0, cycles*len(templates))
	for cycle := 0; cycle < cycles; cycle++ {
		for _, row := range templates {
			row.ID = int64(len(rows) + 1)
			row.ProjectID = screenfixture.Project().ID
			row.ProjectSlug = screenfixture.Project().Slug
			row.CreatedAt = logsGoldenStamp(len(rows))
			rows = append(rows, logsFixtureRegistry.Prepare(row))
		}
	}
	return rows
}

// logsGoldenAuthoredFeed carries only user-authored activity — no tool calls,
// no hooks. It is what makes the empty-state recording reachable through a real
// keystroke: cycling to the tool-calls chip over this feed genuinely returns
// nothing, so the fixture records the empty panel WITH a non-default chip
// bracketed above it rather than a screen that was simply never loaded.
func logsGoldenAuthoredFeed() []domain.EventRow {
	rows := make([]domain.EventRow, 0, 12)
	for _, row := range logsGoldenFeed(1) {
		switch row.Category {
		case domain.EventCategoryTask, domain.EventCategoryComment,
			domain.EventCategoryPlan, domain.EventCategoryTagDep:
			rows = append(rows, logsFixtureRegistry.Prepare(row))
		}
	}
	return rows
}

func logsGoldenCategoryAllowed(categories []domain.EventCategory, eventType string) bool {
	if len(categories) == 0 {
		return true
	}
	actual := logsFixtureRegistry.CategoryOf(eventType)
	for _, category := range categories {
		if category == actual {
			return true
		}
	}
	return false
}

// logsGoldenDeps wires the prepared screen payload the way the host does.
func logsGoldenDeps(rows []domain.EventRow) Deps {
	return Deps{
		Available: true,
		Settings: ViewSettings{
			Retention: Retention{
				Known:      true,
				MaxAgeDays: 7,
				MaxRows:    5000,
				WindowDays: 30,
			},
		},
	}
}

func logsGoldenPayload(rows []domain.EventRow, filter domain.LogsFilterMode) Payload {
	visible := rows
	if filter != FilterAll {
		categories := domain.LogsFilterCategories(filter)
		visible = make([]domain.EventRow, 0, len(rows))
		for _, row := range rows {
			row = logsFixtureRegistry.Prepare(row)
			if logsGoldenCategoryAllowed(categories, row.EventType) {
				visible = append(visible, row)
			}
		}
	} else {
		visible = domain.FilterLogVisibleRows(visible)
	}
	counts := make(map[domain.EventCategory]int, len(domain.KnownEventCategories))
	for _, category := range domain.KnownEventCategories {
		counts[category] = 0
	}
	for _, row := range rows {
		row = logsFixtureRegistry.Prepare(row)
		if category := row.Category; category != domain.EventCategoryUnknown {
			counts[category]++
		}
	}
	return Payload{Rows: visible, Stats: domain.ComputeEventStats(visible, counts)}
}

// logsGoldenBuild materialises the screen the way the host does — bind, run the
// reload, then issue the LifecycleEnter that clamps the scroll window — over a
// feed built afresh on every call, so the determinism proof compares two
// genuinely independent materialisations.
func logsGoldenBuild(feed func() []domain.EventRow) func(screenhost.Frame) screenhost.Screen {
	return func(frame screenhost.Frame) screenhost.Screen {
		logsEnsureEventRegistry()
		rows := feed()
		loaded := New().Bind(logsGoldenDeps(rows)).Apply(logsGoldenPayload(rows, FilterAll))
		return screenfixture.Enter(loaded, frame)
	}
}

// logsGoldenBind re-applies host-owned deps and the prepared filter payload
// between keystrokes.
func logsGoldenBind(feed func() []domain.EventRow) func(screenhost.Screen) screenhost.Screen {
	return func(screen screenhost.Screen) screenhost.Screen {
		rows := feed()
		s := screen.(Screen).Bind(logsGoldenDeps(rows))
		return s.Apply(logsGoldenPayload(rows, s.Filter()))
	}
}

func logsGoldenLongFeed() []domain.EventRow { return logsGoldenFeed(2) }
func logsGoldenDeck() []domain.EventRow     { return logsGoldenFeed(1) }

// logsGoldenControlFeed is the terminal-sink security deck. The values are
// deliberately persisted-shaped: controls occur in comment bodies, JSON title
// and tool-name values, source/entity fields, and a malformed raw payload that
// reaches the unknown-event fallback. Harmless Unicode must survive alongside
// every control family.
func logsGoldenControlFeed() []domain.EventRow {
	rows := []domain.EventRow{
		{
			EntityType: "task\x00", EntityID: 2608,
			EventType:  domain.EventTypeComment,
			AuthorType: "agent\x1b[31m",
			Body:       "comment \x1b]0;owned\a \x00 \n \t \u009b31m \u009dtitle\u009c \u0085 日本語 😀",
		},
		{
			EntityType: "task", EntityID: 2608,
			EventType: domain.EventTypeTaskCreated,
			Source:    "cli\x1b]0;owned\a",
			Payload:   fmt.Sprintf(`{"title":%q,"bucket":"dev","priority":"high"}`, "title \u009b31m 日本語 😀"),
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: domain.EventTypeCLIToolCall,
			Source:    "mcp\x00\u009dsource\u009c",
			Status:    "ok",
			Payload:   fmt.Sprintf(`{"tool_name":%q,"source":"mcp","status":"ok"}`, "tool \u0085 日本語 😀"),
		},
		{
			EntityType: "system", EntityID: 0,
			EventType: "__fixture.controls\x1b[31m",
			Payload:   "payload \x1b]0;owned\a \x00 \u009b31m \u009dosc\u009c 日本語 😀",
		},
	}
	for i := range rows {
		rows[i].ID = int64(i + 1)
		rows[i].ProjectID = screenfixture.Project().ID
		rows[i].ProjectSlug = screenfixture.Project().Slug
		rows[i].CreatedAt = logsGoldenStamp(i)
	}
	return rows
}

// FixtureScenarios returns every recorded state for Stats › Logs.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			Name:  "category-deck",
			Build: logsGoldenBuild(logsGoldenDeck),
			Bind:  logsGoldenBind(logsGoldenDeck),
			Keys:  []string{"j", "j", "j"},
		},
		{
			Name:  "feed-paged",
			Build: logsGoldenBuild(logsGoldenLongFeed),
			Bind:  logsGoldenBind(logsGoldenLongFeed),
			Keys:  []string{"pgdown", "pgdown", "j", "j", "j"},
		},
		{
			Name:  "feed-bottom",
			Build: logsGoldenBuild(logsGoldenLongFeed),
			Bind:  logsGoldenBind(logsGoldenLongFeed),
			Keys:  []string{"G"},
		},
		{
			Name:  "filter-tool-calls",
			Build: logsGoldenBuild(logsGoldenLongFeed),
			Bind:  logsGoldenBind(logsGoldenLongFeed),
			Keys:  []string{"f", "j", "j", "j"},
		},
		{
			Name:  "filter-system",
			Build: logsGoldenBuild(logsGoldenLongFeed),
			Bind:  logsGoldenBind(logsGoldenLongFeed),
			Keys:  []string{"F", "pgdown"},
		},
		{
			Name:  "filter-empty",
			Build: logsGoldenBuild(logsGoldenAuthoredFeed),
			Bind:  logsGoldenBind(logsGoldenAuthoredFeed),
			Keys:  []string{"f"},
		},
		{
			Name:  "control-safety",
			Build: logsGoldenBuild(logsGoldenControlFeed),
			Bind:  logsGoldenBind(logsGoldenControlFeed),
			Keys:  []string{"j"},
		},
	}
}

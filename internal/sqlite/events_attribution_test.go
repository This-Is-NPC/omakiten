package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// taskEventAttribution reads the agent-attribution columns off the most recent
// task event for (projectID, taskID) of the given event type. COALESCE keeps
// the NULL session_id distinguishable from an explicit empty string the same
// way ListRecentEvents does.
func taskEventAttribution(t *testing.T, store *storeFixture, projectID, taskID int64, eventType string) (source, entrypoint, agentModel, agentSessionID string) {
	t.Helper()
	row := store.db.QueryRowContext(context.Background(), `
SELECT COALESCE(source, ''), COALESCE(entrypoint, ''), COALESCE(agent_model, ''), COALESCE(agent_session_id, '')
FROM events
WHERE entity_type = 'task' AND project_id = ? AND entity_id = ? AND event_type = ?
ORDER BY id DESC
LIMIT 1
`, projectID, taskID, eventType)
	if err := row.Scan(&source, &entrypoint, &agentModel, &agentSessionID); err != nil {
		t.Fatalf("taskEventAttribution(%s): %v", eventType, err)
	}
	return source, entrypoint, agentModel, agentSessionID
}

// TestInsertTaskEventStampsAttribution proves the single-source contract: the
// stamping logic lives only inside insertTaskEvent. Driving the package-private
// writer with an attributed ctx must land source/entrypoint/agent_model/
// agent_session_id on the row — none of the five callers thread these as
// parameters.
func TestInsertTaskEventStampsAttribution(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "tasks_move", "claude-opus-4-8", "sess-task")
	store, project := openStoreWithProject(ctx, t)

	task, err := store.CreateTask(context.Background(), project.ID, "t", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	ev, err := insertTaskEvent(ctx, store.db, project.ID, task.ID, domain.EventTypeTaskMoved, "", `{"k":"v"}`)
	if err != nil {
		t.Fatalf("insertTaskEvent: %v", err)
	}

	source, entrypoint, agentModel, agentSessionID := taskEventAttribution(t, store, project.ID, ev.EntityID, domain.EventTypeTaskMoved)
	if source != "mcp" {
		t.Errorf("source = %q, want mcp", source)
	}
	if entrypoint != "tasks_move" {
		t.Errorf("entrypoint = %q, want tasks_move", entrypoint)
	}
	if agentModel != "claude-opus-4-8" {
		t.Errorf("agent_model = %q, want claude-opus-4-8", agentModel)
	}
	if agentSessionID != "sess-task" {
		t.Errorf("agent_session_id = %q, want sess-task", agentSessionID)
	}
}

// TestInsertTaskEventNullSessionWhenAbsent mirrors AddSolution's empty-session
// behaviour: an empty session string is written as NULL, never the literal
// empty string, so GROUP BY queries on agent_session_id stay clean.
func TestInsertTaskEventNullSessionWhenAbsent(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "tasks_move", "claude-sonnet-4-6", "")
	store, project := openStoreWithProject(ctx, t)

	task, err := store.CreateTask(context.Background(), project.ID, "t", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := insertTaskEvent(ctx, store.db, project.ID, task.ID, domain.EventTypeTaskMoved, "", `{}`); err != nil {
		t.Fatalf("insertTaskEvent: %v", err)
	}

	var sessionIsNull bool
	if err := store.db.QueryRowContext(context.Background(), `
SELECT agent_model = ? AND agent_session_id IS NULL
FROM events
WHERE entity_type = 'task' AND project_id = ? AND entity_id = ? AND event_type = ?
ORDER BY id DESC LIMIT 1
`, "claude-sonnet-4-6", project.ID, task.ID, domain.EventTypeTaskMoved).Scan(&sessionIsNull); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !sessionIsNull {
		t.Fatalf("agent_session_id should be NULL (and model stamped) when ctx carries an empty session")
	}
}

// TestTaskEventCharacterizationUnchanged is a characterization test: the
// returned domain.Event shape (the columns RETURNING projects back to callers)
// stays exactly as it was before attribution stamping. insertTaskEvent still
// returns the same id/entity_type/entity_id/project_id/event_type/body/payload/
// created_at tuple, and attribution lives only in the persisted row, not the
// returned struct.
func TestTaskEventCharacterizationUnchanged(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "mcp", "tasks_move", "claude-opus-4-8", "sess-x")
	store, project := openStoreWithProject(ctx, t)

	task, err := store.CreateTask(context.Background(), project.ID, "t", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	ev, err := insertTaskEvent(ctx, store.db, project.ID, task.ID, domain.EventTypeTaskMoved, "body-text", `{"k":"v"}`)
	if err != nil {
		t.Fatalf("insertTaskEvent: %v", err)
	}

	if ev.EntityType != domain.EventEntityTask {
		t.Errorf("entity_type = %q, want %q", ev.EntityType, domain.EventEntityTask)
	}
	if ev.EntityID != task.ID {
		t.Errorf("entity_id = %d, want %d", ev.EntityID, task.ID)
	}
	if ev.ProjectID != project.ID {
		t.Errorf("project_id = %d, want %d", ev.ProjectID, project.ID)
	}
	if ev.EventType != domain.EventTypeTaskMoved {
		t.Errorf("event_type = %q, want %q", ev.EventType, domain.EventTypeTaskMoved)
	}
	if ev.Body != "body-text" {
		t.Errorf("body = %q, want body-text", ev.Body)
	}
	if ev.Payload != `{"k":"v"}` {
		t.Errorf("payload = %q, want %q", ev.Payload, `{"k":"v"}`)
	}
	if ev.ID == 0 || ev.CreatedAt == "" {
		t.Errorf("expected populated id/created_at, got id=%d created_at=%q", ev.ID, ev.CreatedAt)
	}
	// The returned struct deliberately does NOT surface attribution — those
	// columns are persisted but not part of the RETURNING projection, so the
	// caller-facing shape is byte-identical to the pre-stamping contract.
	if ev.AgentModel != "" || ev.AgentSessionID != "" || ev.Source != "" || ev.Entrypoint != "" {
		t.Errorf("returned Event must not carry attribution: %+v", ev)
	}
}

// TestTaskEventPathsStampAttribution drives the five distinct call sites that
// reach insertTaskEvent and asserts each persists attribution. Because the
// stamping is single-source, every path inherits it for free by passing its
// existing ctx:
//
//	1. RecordTaskEvent      (events.go)
//	2. CreateTask -> txevent (txevent.go, EventScopeTask)
//	3. MoveTask             (tasks.go)
//	4. SetTaskState         (tasks_lifecycle.go)
//	5. RebindOrphanedTasks  (orphans.go)
func TestTaskEventPathsStampAttribution(t *testing.T) {
	const (
		wantModel   = "claude-opus-4-8"
		wantSession = "sess-paths"
	)
	ctx := activity.WithAgent(context.Background(), "mcp", "okt", wantModel, wantSession)

	assertStamped := func(t *testing.T, store *storeFixture, projectID, taskID int64, eventType string) {
		t.Helper()
		_, _, model, session := taskEventAttribution(t, store, projectID, taskID, eventType)
		if model != wantModel {
			t.Errorf("%s: agent_model = %q, want %q", eventType, model, wantModel)
		}
		if session != wantSession {
			t.Errorf("%s: agent_session_id = %q, want %q", eventType, session, wantSession)
		}
	}

	t.Run("RecordTaskEvent", func(t *testing.T) {
		store, project := openStoreWithProject(ctx, t)
		task, err := store.CreateTask(context.Background(), project.ID, "t", "", domain.Priority(2), "backlog", nil, store.snap())
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if _, err := store.RecordTaskEvent(ctx, project.ID, task.ID, domain.EventTypeTaskMoved, "", `{}`); err != nil {
			t.Fatalf("RecordTaskEvent: %v", err)
		}
		assertStamped(t, store, project.ID, task.ID, domain.EventTypeTaskMoved)
	})

	t.Run("CreateTaskThroughTxEvent", func(t *testing.T) {
		store, project := openStoreWithProject(ctx, t)
		task, err := store.CreateTask(ctx, project.ID, "t", "", domain.Priority(2), "backlog", nil, store.snap())
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		assertStamped(t, store, project.ID, task.ID, domain.EventTypeTaskCreated)
	})

	t.Run("MoveTask", func(t *testing.T) {
		store, project := openStoreWithFullTransitions(context.Background(), t)
		task, err := store.CreateTask(context.Background(), project.ID, "t", "", domain.Priority(2), "backlog", nil, store.snap())
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev", store.snap()); err != nil {
			t.Fatalf("MoveTask: %v", err)
		}
		assertStamped(t, store, project.ID, task.ID, domain.EventTypeTaskMoved)
	})

	t.Run("SetTaskState", func(t *testing.T) {
		store, project := openStoreWithProject(ctx, t)
		task, err := store.CreateTask(context.Background(), project.ID, "t", "", domain.Priority(2), "backlog", nil, store.snap())
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if _, _, err := store.SetTaskState(ctx, project.ID, task.ID, domain.TaskStateArchived, "", store.snap()); err != nil {
			t.Fatalf("SetTaskState: %v", err)
		}
		assertStamped(t, store, project.ID, task.ID, domain.EventTypeTaskArchived)
	})

	t.Run("RebindOrphanedTasks", func(t *testing.T) {
		store := newTestStore(t)
		bundleA := bundleWithKeys(t, "preset_a", []string{"docs"}, []int{1})
		bundleA.Kit.Key = "preset_a"
		bundleA.Kit.Name = "Preset A"
		store.applyBundle(bundleA)
		project := mustUpsertProject(t, store, "p", "p", "/p")
		task, err := store.CreateTask(context.Background(), project.ID, "doc work", "", domain.Priority(2), "docs", nil, store.snap())
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}

		bundleB := bundleWithKeys(t, "preset_b", []string{"backlog"}, []int{1})
		bundleB.Kit.Key = "preset_b"
		bundleB.Kit.Name = "Preset B"
		store.applyBundle(bundleB)

		report, err := store.RebindOrphanedTasks(ctx, project.ID, store.snap(), store.prev())
		if err != nil {
			t.Fatalf("RebindOrphanedTasks: %v", err)
		}
		if report.Total != 1 {
			t.Fatalf("orphan Total = %d, want 1", report.Total)
		}
		_, _, model, session := taskEventAttribution(t, store, project.ID, task.ID, domain.EventTypeTaskMigrated)
		if model != wantModel || session != wantSession {
			t.Errorf("orphan event attribution = (%q,%q), want (%q,%q)", model, session, wantModel, wantSession)
		}
	})
}

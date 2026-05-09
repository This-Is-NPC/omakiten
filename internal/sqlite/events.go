package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// RecordTaskEvent inserts an entity_type='task' event tied to a task. Used by
// the service layer to record task.created / task.moved / task.completed.
// Returns the persisted Event so callers can fold it into the activity feed
// without an extra read. Comments go through AddComment instead — they share
// the same table but require tag handling.
//
// When the configured events policy resolves Log=false for eventType the
// row is dropped silently — callers receive a zero Event and nil error so
// telemetry gating cannot break business logic.
func (s *Store) RecordTaskEvent(ctx context.Context, projectID, taskID int64, eventType, body, payload string) (domain.Event, error) {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return domain.Event{}, err
	}
	if !s.shouldLogEvent(eventType) {
		return domain.Event{}, nil
	}
	return insertTaskEvent(ctx, s.db, projectID, taskID, eventType, body, payload)
}

// dbExecutor abstracts over *sql.DB and *sql.Tx so insertTaskEvent can run
// inline in MoveTask's transaction without forcing callers to pick.
type dbExecutor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// RecordEntityEvent persists a domain event with attribution pulled from ctx.
// Used by ErrorService to emit error.recorded, solution.added, solution.liked,
// and other domain events. entityType="system" for events that don't tie to
// a single row (e.g. solution.viewed_top — listing the top solutions). When
// entityID is 0 we write NULL so foreign-key-style filters stay sane.
// ListRecentEvents returns the most recent domain events filtered by
// event_type, ordered newest-first. Used by tests to assert emission and
// (eventually) by metrics.summary to introspect timelines. Callers that
// pass <=0 inherit the configured fallback (config.events.default_recent_limit,
// wired in by SetEventsRecentLimit at composition root). When neither
// the caller nor the composition root supplied a value, falls back to
// the kit canonical so tests using bare sqlite.Open still work.
func (s *Store) ListRecentEvents(ctx context.Context, eventType string, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		limit = s.eventsDefaultRecentLimit
	}
	if limit <= 0 {
		// Composition root forgot to wire SetEventsRecentLimit; fall
		// through to the kit canonical so the query still runs. Production
		// always sets a positive value via the runtime bootstrap.
		if cfg, err := config.LoadKitConfig(); err == nil && cfg.Events.DefaultRecentLimit > 0 {
			limit = cfg.Events.DefaultRecentLimit
		}
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, entity_type, COALESCE(entity_id, 0), COALESCE(project_id, 0), event_type, COALESCE(body, ''), COALESCE(payload, ''), COALESCE(source, ''), COALESCE(entrypoint, ''), COALESCE(agent_model, ''), COALESCE(agent_session_id, ''), created_at
FROM events
WHERE event_type = ?
ORDER BY created_at DESC, id DESC
LIMIT ?
`, eventType, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		if err := rows.Scan(&ev.ID, &ev.EntityType, &ev.EntityID, &ev.ProjectID, &ev.EventType, &ev.Body, &ev.Payload, &ev.Source, &ev.Entrypoint, &ev.AgentModel, &ev.AgentSessionID, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) RecordEntityEvent(ctx context.Context, entityType string, entityID int64, projectID int64, eventType string, payload string) error {
	if !s.shouldLogEvent(eventType) {
		return nil
	}
	if payload == "" {
		payload = "{}"
	}
	source, entrypoint, agentModel, agentSessionID := agentAttribution(ctx)
	var entityIDArg, projectIDArg any
	if entityID > 0 {
		entityIDArg = entityID
	}
	if projectID > 0 {
		projectIDArg = projectID
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events(entity_type, entity_id, project_id, event_type, payload, source, entrypoint, agent_model, agent_session_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, entityType, entityIDArg, projectIDArg, eventType, payload, source, entrypoint, agentModel, agentSessionID)
	return err
}

func insertTaskEvent(ctx context.Context, exec dbExecutor, projectID, taskID int64, eventType, body, payload string) (domain.Event, error) {
	if payload == "" {
		payload = "{}"
	}
	var event domain.Event
	if err := exec.QueryRowContext(ctx, `
INSERT INTO events(entity_type, entity_id, project_id, event_type, body, payload)
VALUES ('task', ?, ?, ?, ?, ?)
RETURNING id, entity_type, entity_id, project_id, event_type, body, payload, created_at
`, taskID, projectID, eventType, body, payload).Scan(
		&event.ID, &event.EntityType, &event.EntityID, &event.ProjectID,
		&event.EventType, &event.Body, &event.Payload, &event.CreatedAt,
	); err != nil {
		return domain.Event{}, fmt.Errorf("record task event: %w", err)
	}
	return event, nil
}

// ListTaskActivity returns the unified feed for a single task: comments and
// system events (task.created/moved/completed) ordered by created_at. Order
// is "asc" (oldest first) or "desc" (newest first). Tags are eager-loaded
// for comment rows so the TUI can render them without a follow-up query.
func (s *Store) ListTaskActivity(ctx context.Context, projectID, taskID int64, order string) ([]domain.Event, error) {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return nil, err
	}
	direction := "ASC"
	if strings.EqualFold(order, "desc") {
		direction = "DESC"
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, entity_type, entity_id, COALESCE(project_id, 0), event_type, body, payload, COALESCE(author_type, ''), created_at
FROM events
WHERE entity_type = 'task' AND project_id = ? AND entity_id = ?
ORDER BY created_at `+direction+`, id `+direction+`
`, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []domain.Event
	for rows.Next() {
		var ev domain.Event
		var authorType sql.NullString
		if err := rows.Scan(&ev.ID, &ev.EntityType, &ev.EntityID, &ev.ProjectID,
			&ev.EventType, &ev.Body, &ev.Payload, &authorType, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if authorType.Valid {
			ev.AuthorType = authorType.String
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Eager-load tags only for rows that can carry them (comments today;
	// system events stay tag-less by design).
	if len(events) > 0 {
		commentIDs := make([]int64, 0, len(events))
		for _, ev := range events {
			if ev.EventType == domain.EventTypeComment {
				commentIDs = append(commentIDs, ev.ID)
			}
		}
		if len(commentIDs) > 0 {
			tagsByEvent, err := s.eventTagsByIDs(ctx, commentIDs)
			if err != nil {
				return nil, err
			}
			for i := range events {
				if tags, ok := tagsByEvent[events[i].ID]; ok {
					events[i].Tags = tags
				}
			}
		}
	}
	return events, nil
}

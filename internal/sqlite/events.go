package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

// RecordTaskEvent inserts an entity_type='task' event tied to a task. Used by
// the service layer to record task.created / task.moved / task.completed.
// Returns the persisted Event so callers can fold it into the activity feed
// without an extra read. Comments go through AddComment instead — they share
// the same table but require tag handling.
func (s *Store) RecordTaskEvent(ctx context.Context, projectID, taskID int64, eventType, body, payload string) (domain.Event, error) {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return domain.Event{}, err
	}
	return insertTaskEvent(ctx, s.db, projectID, taskID, eventType, body, payload)
}

// dbExecutor abstracts over *sql.DB and *sql.Tx so insertTaskEvent can run
// inline in MoveTask's transaction without forcing callers to pick.
type dbExecutor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
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

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"omakiten/internal/domain"
)

func (s *Store) AddComment(ctx context.Context, projectID, taskID int64, body, authorType string, tags []domain.Tag) (domain.Comment, error) {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return domain.Comment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Comment{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var comment domain.Comment
	if err := tx.QueryRowContext(ctx, `
INSERT INTO events(entity_type, entity_id, project_id, event_type, body, author_type)
VALUES ('task', ?, ?, 'comment', ?, ?)
RETURNING id, project_id, entity_id, body, author_type, created_at
`, taskID, projectID, body, authorType).Scan(&comment.ID, &comment.ProjectID, &comment.TaskID, &comment.Body, &comment.AuthorType, &comment.CreatedAt); err != nil {
		return domain.Comment{}, err
	}

	attached, err := attachTagsTx(ctx, tx, tagPivotEvent, comment.ID, tags)
	if err != nil {
		return domain.Comment{}, err
	}
	comment.Tags = attached

	if err := tx.Commit(); err != nil {
		return domain.Comment{}, err
	}
	s.publishEvent(ctx, domain.Event{
		ID:         comment.ID,
		EntityType: domain.EventEntityTask,
		EntityID:   comment.TaskID,
		ProjectID:  comment.ProjectID,
		EventType:  domain.EventTypeComment,
		Body:       comment.Body,
		AuthorType: comment.AuthorType,
	})
	return comment, nil
}

func (s *Store) ListComments(ctx context.Context, projectID, taskID int64) ([]domain.Comment, error) {
	query := "SELECT id, project_id, entity_id, body, author_type, created_at FROM events WHERE entity_type = 'task' AND event_type = 'comment' AND project_id = ?"
	args := []any{projectID}
	if taskID > 0 {
		if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
			return nil, err
		}
		query += " AND entity_id = ?"
		args = append(args, taskID)
	}
	query += " ORDER BY id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var comments []domain.Comment
	for rows.Next() {
		var comment domain.Comment
		if err := rows.Scan(&comment.ID, &comment.ProjectID, &comment.TaskID, &comment.Body, &comment.AuthorType, &comment.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(comments) > 0 {
		ids := make([]int64, len(comments))
		for i, c := range comments {
			ids[i] = c.ID
		}
		tagsByEvent, err := s.eventTagsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for i := range comments {
			if tags, ok := tagsByEvent[comments[i].ID]; ok {
				comments[i].Tags = tags
			}
		}
	}

	return comments, nil
}

// UpdateComment rewrites a comment's body and replaces its tags. Emits a
// comment.edited event tied to the parent task with a payload that names the
// changed fields. Tag replacement clears event_tags for the comment then
// re-applies the supplied list (deduped, normalized by the caller).
func (s *Store) UpdateComment(ctx context.Context, projectID, commentID int64, body string, tags []domain.Tag) (domain.Comment, domain.Event, error) {
	type updateResult struct {
		updated domain.Comment
		prev    domain.Comment
	}
	out, err := txMutateAndEmit(ctx, s, TxMutation[updateResult]{
		Scope:     EventScopeTask,
		EventType: domain.EventTypeCommentEdited,
		ProjectID: projectID,
		EntityID:  func(r updateResult) int64 { return r.updated.TaskID },
		ShouldLog: func() bool { return s.shouldLogEvent(domain.EventTypeCommentEdited) },
		Mutate: func(ctx context.Context, tx *sql.Tx) (updateResult, error) {
			prev, err := commentByIDTx(ctx, tx, projectID, commentID)
			if err != nil {
				return updateResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE events SET body = ? WHERE id = ? AND project_id = ? AND entity_type = 'task' AND event_type = 'comment'
`, body, commentID, projectID); err != nil {
				return updateResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM event_tags WHERE event_id = ?`, commentID); err != nil {
				return updateResult{}, err
			}
			updated := domain.Comment{
				ID:         prev.ID,
				ProjectID:  prev.ProjectID,
				TaskID:     prev.TaskID,
				Body:       body,
				AuthorType: prev.AuthorType,
				CreatedAt:  prev.CreatedAt,
			}
			attached, err := attachTagsTx(ctx, tx, tagPivotEvent, commentID, tags)
			if err != nil {
				return updateResult{}, err
			}
			updated.Tags = attached
			return updateResult{updated: updated, prev: prev}, nil
		},
		Payload: func(r updateResult) (string, error) {
			payload := map[string]any{"comment_id": commentID}
			if r.prev.Body != body {
				payload["body"] = map[string]any{"from": r.prev.Body, "to": body}
			}
			b, err := json.Marshal(payload)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	})
	if err != nil {
		return domain.Comment{}, domain.Event{}, err
	}
	// Reconstruct the envelope callers expect (matches the pre-helper
	// publish payload — id/created_at omitted but no caller reads them
	// today).
	event := domain.Event{
		EntityType: domain.EventEntityTask,
		EntityID:   out.updated.TaskID,
		ProjectID:  projectID,
		EventType:  domain.EventTypeCommentEdited,
	}
	return out.updated, event, nil
}

// DeleteComment hard-deletes a comment (including its event_tags via FK
// cascade) and emits a comment.removed event with the body snapshot tied to
// the parent task so the activity feed retains an audit trail.
func (s *Store) DeleteComment(ctx context.Context, projectID, commentID int64) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer func() { _ = tx.Rollback() }()

	prev, err := commentByIDTx(ctx, tx, projectID, commentID)
	if err != nil {
		return domain.Event{}, err
	}

	if _, err := tx.ExecContext(ctx, `
DELETE FROM events WHERE id = ? AND project_id = ? AND entity_type = 'task' AND event_type = 'comment'
`, commentID, projectID); err != nil {
		return domain.Event{}, err
	}

	payload, marshalErr := json.Marshal(map[string]any{
		"comment_id":  commentID,
		"author_type": prev.AuthorType,
		"body":        prev.Body,
	})
	if marshalErr != nil {
		return domain.Event{}, marshalErr
	}
	var event domain.Event
	if s.shouldLogEvent(domain.EventTypeCommentRemoved) {
		event, err = insertTaskEvent(ctx, tx, projectID, prev.TaskID, domain.EventTypeCommentRemoved, "", string(payload))
		if err != nil {
			return domain.Event{}, fmt.Errorf("emit comment.removed: %w", err)
		}
	} else {
		event = domain.Event{EntityType: domain.EventEntityTask, EntityID: prev.TaskID, ProjectID: projectID, EventType: domain.EventTypeCommentRemoved, Payload: string(payload)}
	}

	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	s.publishEvent(ctx, event)
	return event, nil
}

// CommentByID returns a single comment row scoped to the active project.
func (s *Store) CommentByID(ctx context.Context, projectID, commentID int64) (domain.Comment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Comment{}, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := commentByIDTx(ctx, tx, projectID, commentID)
	if err != nil {
		return domain.Comment{}, err
	}
	return c, tx.Commit()
}

func commentByIDTx(ctx context.Context, tx *sql.Tx, projectID, commentID int64) (domain.Comment, error) {
	var c domain.Comment
	err := tx.QueryRowContext(ctx, `
SELECT id, project_id, entity_id, body, COALESCE(author_type, ''), created_at
FROM events
WHERE id = ? AND project_id = ? AND entity_type = 'task' AND event_type = 'comment'
`, commentID, projectID).Scan(&c.ID, &c.ProjectID, &c.TaskID, &c.Body, &c.AuthorType, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Comment{}, domain.NewError(domain.ErrValidation, "comment not found", map[string]any{"comment_id": commentID, "project_id": projectID})
		}
		return domain.Comment{}, err
	}
	return c, nil
}

func (s *Store) eventTagsByIDs(ctx context.Context, eventIDs []int64) (map[int64][]domain.Tag, error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT et.event_id, t.id, t.name, t.label FROM event_tags et JOIN tags t ON t.id = et.tag_id WHERE et.event_id IN ("+placeholders(len(eventIDs))+")",
		args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := map[int64][]domain.Tag{}
	for rows.Next() {
		var eventID int64
		var tag domain.Tag
		if err := rows.Scan(&eventID, &tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		result[eventID] = append(result[eventID], tag)
	}
	return result, rows.Err()
}

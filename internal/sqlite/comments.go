package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

// commentSelectColumns is the shared projection for comment reads. project_id
// and entity_id are NULL for universal comments, so both are COALESCEd; the
// remaining note-like fields default to NULL on legacy rows.
const commentSelectColumns = `id, COALESCE(project_id, 0), entity_type, COALESCE(entity_id, 0), body, ` +
	`COALESCE(title, ''), COALESCE(kind, ''), COALESCE(pinned, 0), ` +
	`COALESCE(author_type, ''), created_at, COALESCE(updated_at, '')`

// commentSelectColumnsE is commentSelectColumns qualified with the `e` table
// alias used by QueryComments, whose JOINs (tags, search_index) would otherwise
// make bare `id` ambiguous.
const commentSelectColumnsE = `e.id, COALESCE(e.project_id, 0), e.entity_type, COALESCE(e.entity_id, 0), e.body, ` +
	`COALESCE(e.title, ''), COALESCE(e.kind, ''), COALESCE(e.pinned, 0), ` +
	`COALESCE(e.author_type, ''), e.created_at, COALESCE(e.updated_at, '')`

// scanComment reads a row produced by commentSelectColumns into a domain
// comment and derives Scope/TaskID from entity_type. Project-scoped comments
// store the project id in entity_id; only task-scoped rows expose a TaskID.
func scanComment(scan func(dest ...any) error) (domain.Comment, error) {
	var c domain.Comment
	var entityType string
	var entityID int64
	var pinned int
	if err := scan(&c.ID, &c.ProjectID, &entityType, &entityID, &c.Body,
		&c.Title, &c.Kind, &pinned, &c.AuthorType, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return domain.Comment{}, err
	}
	c.Scope = entityType
	c.Pinned = pinned != 0
	if entityType == domain.CommentScopeTask {
		c.TaskID = entityID
	}
	return c, nil
}

// AddComment is the task-scope convenience wrapper retained for existing
// callers (TUI atomics, tests). It delegates to AddScopedComment.
func (s *Store) AddComment(ctx context.Context, projectID, taskID int64, body, authorType string, tags []domain.Tag) (domain.Comment, error) {
	return s.AddScopedComment(ctx, domain.CommentWrite{
		Scope:      domain.CommentScopeTask,
		ProjectID:  projectID,
		TaskID:     taskID,
		Body:       body,
		AuthorType: authorType,
		Tags:       tags,
	})
}

// AddScopedComment inserts a comment at the requested scope. task comments go
// through the existing ensureTaskExists guard; project/universal comments skip
// the task check. entity_id/project_id are mapped per scope: project comments
// carry the project id in entity_id, universal comments carry NULL in both.
func (s *Store) AddScopedComment(ctx context.Context, w domain.CommentWrite) (domain.Comment, error) {
	scope := w.Scope
	if scope == "" {
		scope = domain.CommentScopeTask
	}

	var entityIDArg, projectIDArg any
	switch scope {
	case domain.CommentScopeTask:
		if err := s.ensureTaskExists(ctx, w.ProjectID, w.TaskID); err != nil {
			return domain.Comment{}, err
		}
		entityIDArg = w.TaskID
		projectIDArg = w.ProjectID
	case domain.CommentScopeProject:
		if w.ProjectID <= 0 {
			return domain.Comment{}, domain.NewError(domain.ErrValidation, "project comment requires a project id", nil)
		}
		entityIDArg = w.ProjectID
		projectIDArg = w.ProjectID
	case domain.CommentScopeUniversal:
		entityIDArg = nil
		projectIDArg = nil
	default:
		return domain.Comment{}, domain.NewError(domain.ErrValidation, "unknown comment scope", map[string]any{"scope": w.Scope})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Comment{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var titleArg, kindArg any
	if w.Title != "" {
		titleArg = w.Title
	}
	if w.Kind != "" {
		kindArg = w.Kind
	}

	var id int64
	var createdAt string
	if err := tx.QueryRowContext(ctx, `
INSERT INTO events(entity_type, entity_id, project_id, event_type, body, title, kind, pinned, author_type)
VALUES (?, ?, ?, 'comment', ?, ?, ?, ?, ?)
RETURNING id, created_at
`, scope, entityIDArg, projectIDArg, w.Body, titleArg, kindArg, boolToInt(w.Pinned), w.AuthorType).Scan(&id, &createdAt); err != nil {
		return domain.Comment{}, err
	}

	comment := domain.Comment{
		ID:         id,
		ProjectID:  w.ProjectID,
		Scope:      scope,
		Body:       w.Body,
		Title:      w.Title,
		Kind:       w.Kind,
		Pinned:     w.Pinned,
		AuthorType: w.AuthorType,
		CreatedAt:  createdAt,
	}
	if scope == domain.CommentScopeUniversal {
		comment.ProjectID = 0
	}
	if scope == domain.CommentScopeTask {
		comment.TaskID = w.TaskID
	}

	attached, err := attachTagsTx(ctx, tx, tagPivotEvent, comment.ID, w.Tags)
	if err != nil {
		return domain.Comment{}, err
	}
	comment.Tags = attached

	if err := tx.Commit(); err != nil {
		return domain.Comment{}, err
	}
	s.publishEvent(ctx, domain.Event{
		ID:         comment.ID,
		EntityType: scope,
		EntityID:   comment.TaskID,
		ProjectID:  comment.ProjectID,
		EventType:  domain.EventTypeComment,
		Body:       comment.Body,
		AuthorType: comment.AuthorType,
	})
	return comment, nil
}

// ListComments returns task-scoped comments for a project. taskID=0 lists every
// task comment in the project (the per-project task-comment feed); a positive
// taskID narrows to a single task. Project/universal comments are out of scope
// here — use QueryComments for the cross-cutting handoff log.
func (s *Store) ListComments(ctx context.Context, projectID, taskID int64) ([]domain.Comment, error) {
	query := "SELECT " + commentSelectColumns + " FROM events WHERE entity_type = 'task' AND event_type = 'comment' AND project_id = ?"
	args := []any{projectID}
	if taskID > 0 {
		if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
			return nil, err
		}
		query += " AND entity_id = ?"
		args = append(args, taskID)
	}
	query += " ORDER BY id"
	return s.queryCommentRows(ctx, query, args)
}

// QueryComments is the filterable handoff-log surface. The filter fields AND
// together. Scope, kind, pinned, and the created_at window filter on events
// columns; Tag joins event_tags; Search runs an FTS5 MATCH against the unified
// search_index (which indexes body+title for comment rows, migration 032).
func (s *Store) QueryComments(ctx context.Context, filter domain.CommentFilter) ([]domain.Comment, error) {
	var b strings.Builder
	b.WriteString("SELECT " + commentSelectColumnsE + " FROM events e")

	var conds []string
	var args []any

	if filter.Tag != "" {
		b.WriteString(" JOIN event_tags et ON et.event_id = e.id JOIN tags t ON t.id = et.tag_id")
		conds = append(conds, "t.name = ?")
		args = append(args, filter.Tag)
	}
	if filter.Search != "" {
		b.WriteString(" JOIN search_index si ON si.entity_type = 'comment' AND si.entity_id = e.id")
		conds = append(conds, "search_index MATCH ?")
		args = append(args, filter.Search)
	}

	conds = append(conds, "e.event_type = 'comment'")
	if filter.Scope != "" {
		conds = append(conds, "e.entity_type = ?")
		args = append(args, filter.Scope)
	}
	if filter.ProjectID > 0 {
		conds = append(conds, "e.project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.TaskID > 0 {
		conds = append(conds, "e.entity_type = 'task' AND e.entity_id = ?")
		args = append(args, filter.TaskID)
	}
	if filter.Kind != "" {
		conds = append(conds, "e.kind = ?")
		args = append(args, filter.Kind)
	}
	if filter.PinnedOnly {
		conds = append(conds, "e.pinned = 1")
	}
	// created_at is stored "YYYY-MM-DD HH:MM:SS" (space separator); bounds may
	// arrive as RFC3339 ("...T...Z"), which would sort wrong under a raw string
	// compare. datetime() normalizes both the column and the bound to a common
	// shape so the window comparison is chronological, not lexicographic.
	if filter.CreatedAfter != "" {
		conds = append(conds, "datetime(e.created_at) >= datetime(?)")
		args = append(args, filter.CreatedAfter)
	}
	if filter.CreatedBefore != "" {
		conds = append(conds, "datetime(e.created_at) <= datetime(?)")
		args = append(args, filter.CreatedBefore)
	}

	b.WriteString(" WHERE " + strings.Join(conds, " AND "))
	b.WriteString(" ORDER BY e.created_at, e.id")

	return s.queryCommentRows(ctx, b.String(), args)
}

// queryCommentRows runs a comment SELECT built on commentSelectColumns, scans
// each row, and eager-loads tags for the result set.
func (s *Store) queryCommentRows(ctx context.Context, query string, args []any) ([]domain.Comment, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var comments []domain.Comment
	for rows.Next() {
		comment, err := scanComment(rows.Scan)
		if err != nil {
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

// UpdateComment rewrites a comment's body and replaces its tags. It is the
// body-only path retained for the existing service Edit flow; EditComment is
// the wider scope-agnostic patch. Emits a comment.edited event tied to the
// parent task with a payload that names the changed fields.
func (s *Store) UpdateComment(ctx context.Context, projectID, commentID int64, body string, tags []domain.Tag) (domain.Comment, domain.Event, error) {
	return s.EditComment(ctx, projectID, commentID, domain.CommentEdit{Body: body, Tags: tags})
}

// EditComment applies the scope-agnostic patch (body/title/kind/pinned + tags),
// stamps updated_at, and emits comment.edited. Works for task, project, and
// universal comments — the WHERE clause filters on event_type='comment' only,
// not entity_type, so non-task scopes are editable. For task comments the
// emitted event is tied to the parent task; project/universal edits emit under
// their own entity scope.
func (s *Store) EditComment(ctx context.Context, projectID, commentID int64, edit domain.CommentEdit) (domain.Comment, domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Comment{}, domain.Event{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	prev, err := commentByIDTx(ctx, tx, projectID, commentID)
	if err != nil {
		return domain.Comment{}, domain.Event{}, err
	}

	// Tri-state patch: a nil Title/Kind/Pinned pointer preserves the loaded
	// row's existing value so a body-only edit can't silently wipe a title,
	// kind, or pinned flag. Only an explicit non-nil pointer overwrites.
	newTitle := prev.Title
	if edit.Title != nil {
		newTitle = *edit.Title
	}
	newKind := prev.Kind
	if edit.Kind != nil {
		newKind = *edit.Kind
	}
	newPinned := prev.Pinned
	if edit.Pinned != nil {
		newPinned = *edit.Pinned
	}

	var titleArg, kindArg any
	if newTitle != "" {
		titleArg = newTitle
	}
	if newKind != "" {
		kindArg = newKind
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE events SET body = ?, title = ?, kind = ?, pinned = ?, updated_at = datetime('now')
WHERE id = ? AND event_type = 'comment'
`, edit.Body, titleArg, kindArg, boolToInt(newPinned), commentID); err != nil {
		return domain.Comment{}, domain.Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM event_tags WHERE event_id = ?`, commentID); err != nil {
		return domain.Comment{}, domain.Event{}, err
	}

	updated := prev
	updated.Body = edit.Body
	updated.Title = newTitle
	updated.Kind = newKind
	updated.Pinned = newPinned
	attached, err := attachTagsTx(ctx, tx, tagPivotEvent, commentID, edit.Tags)
	if err != nil {
		return domain.Comment{}, domain.Event{}, err
	}
	updated.Tags = attached

	payload := map[string]any{"comment_id": commentID}
	if prev.Body != edit.Body {
		payload["body"] = map[string]any{"from": prev.Body, "to": edit.Body}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return domain.Comment{}, domain.Event{}, err
	}

	var event domain.Event
	if s.shouldLogEvent(domain.EventTypeCommentEdited) {
		event, err = insertEntityEvent(ctx, tx, updated.Scope, entityIDForScope(updated), projectID, domain.EventTypeCommentEdited, string(payloadJSON))
		if err != nil {
			return domain.Comment{}, domain.Event{}, fmt.Errorf("emit comment.edited: %w", err)
		}
	} else {
		event = domain.Event{EntityType: updated.Scope, EntityID: entityIDForScope(updated), ProjectID: projectID, EventType: domain.EventTypeCommentEdited, Payload: string(payloadJSON)}
	}

	if err := tx.Commit(); err != nil {
		return domain.Comment{}, domain.Event{}, err
	}
	committed = true
	s.publishEvent(ctx, event)
	return updated, event, nil
}

// entityIDForScope resolves the events.entity_id the comment's event row points
// at: the task id for task scope, the project id for project scope, and 0
// (NULL) for universal scope.
func entityIDForScope(c domain.Comment) int64 {
	switch c.Scope {
	case domain.CommentScopeTask:
		return c.TaskID
	case domain.CommentScopeProject:
		return c.ProjectID
	default:
		return 0
	}
}

// DeleteComment hard-deletes a comment (including its event_tags via FK
// cascade) and emits a comment.removed event with the body snapshot tied to the
// comment's scope so the activity feed retains an audit trail. Scope-agnostic:
// the WHERE clause filters on event_type='comment', so project and universal
// comments delete too.
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
DELETE FROM events WHERE id = ? AND event_type = 'comment'
`, commentID); err != nil {
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
		event, err = insertEntityEvent(ctx, tx, prev.Scope, entityIDForScope(prev), projectID, domain.EventTypeCommentRemoved, string(payload))
		if err != nil {
			return domain.Event{}, fmt.Errorf("emit comment.removed: %w", err)
		}
	} else {
		event = domain.Event{EntityType: prev.Scope, EntityID: entityIDForScope(prev), ProjectID: projectID, EventType: domain.EventTypeCommentRemoved, Payload: string(payload)}
	}

	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	s.publishEvent(ctx, event)
	return event, nil
}

// CommentByID returns a single comment row. Reads across all scopes; the
// project filter only constrains task/project-scoped rows because universal
// comments carry a NULL project_id.
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
	c, err := scanComment(tx.QueryRowContext(ctx, `
SELECT `+commentSelectColumns+`
FROM events
WHERE id = ? AND event_type = 'comment' AND (project_id = ? OR project_id IS NULL)
`, commentID, projectID).Scan)
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

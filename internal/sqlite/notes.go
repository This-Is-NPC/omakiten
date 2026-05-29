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

// CreateNote inserts a new note row. When projectID == 0 the row is
// global (project_id IS NULL); otherwise it belongs to the supplied
// project and cascades on project delete (FK from migration 031).
// Tags are best-effort: each name is upserted into the global `tags`
// table and linked through notes_tags.
//
// The triggers from 031 keep notes_fts and search_index consistent
// without any extra writes from the service layer.
func (s *Store) CreateNote(ctx context.Context, projectID int64, kind, title, body string, pinned bool, authorModel string, tags []domain.Tag) (domain.Note, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Note{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var projectArg any
	if projectID > 0 {
		projectArg = projectID
	}

	var note domain.Note
	var nullableProject sql.NullInt64
	var nullableAuthor sql.NullString
	if err := tx.QueryRowContext(ctx, `
INSERT INTO notes(project_id, kind, title, body, pinned, author_model, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING id, project_id, kind, title, body, pinned, author_model, created_at, updated_at
`, projectArg, kind, title, body, boolToInt(pinned), nullStringIfEmpty(authorModel)).Scan(
		&note.ID, &nullableProject, &note.Kind, &note.Title, &note.Body, &note.Pinned, &nullableAuthor, &note.CreatedAt, &note.UpdatedAt,
	); err != nil {
		return domain.Note{}, mapNoteSQLiteError(err)
	}
	if nullableProject.Valid {
		note.ProjectID = nullableProject.Int64
	}
	if nullableAuthor.Valid {
		note.AuthorModel = nullableAuthor.String
	}

	attached, err := attachNoteTagsTx(ctx, tx, note.ID, tags)
	if err != nil {
		return domain.Note{}, err
	}
	note.Tags = attached

	// Emit note.created inside the same tx so a downstream commit
	// failure leaves the events table consistent with the notes table —
	// mirrors the tasks.CreateTask pattern. The payload is rebuilt from
	// the post-INSERT note so scope/tags reflect the persisted state.
	createdEv, err := emitNoteEventTx(ctx, s, tx, note, domain.EventTypeNoteCreated, nil)
	if err != nil {
		return domain.Note{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Note{}, err
	}
	committed = true
	if createdEv.EventType != "" {
		s.publishEvent(ctx, createdEv)
	}
	return note, nil
}

// UpdateNote applies a patch in a single transaction. Each pointer
// field on the update encodes the omitted-vs-explicit distinction so
// callers can rewrite a single field without first reading the row.
// Tags is full replacement: passing an empty slice clears every tag,
// passing nil leaves the existing set in place.
func (s *Store) UpdateNote(ctx context.Context, id int64, update domain.NoteUpdate) (domain.Note, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Note{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	previous, err := noteByIDTx(ctx, tx, id)
	if err != nil {
		return domain.Note{}, err
	}

	sets := []string{}
	args := []any{}
	if update.Title != nil && *update.Title != previous.Title {
		sets = append(sets, "title = ?")
		args = append(args, *update.Title)
	}
	if update.Body != nil && *update.Body != previous.Body {
		sets = append(sets, "body = ?")
		args = append(args, *update.Body)
	}
	if update.Kind != nil && *update.Kind != previous.Kind {
		sets = append(sets, "kind = ?")
		args = append(args, *update.Kind)
	}
	if update.Pinned != nil && *update.Pinned != previous.Pinned {
		sets = append(sets, "pinned = ?")
		args = append(args, boolToInt(*update.Pinned))
	}

	tagsChanged := update.Tags != nil && !sameTagSet(previous.Tags, *update.Tags)

	// No-op short-circuit: when no scalar field differs AND the tag
	// patch (if any) matches the current set, skip the UPDATE and the
	// note.edited emit entirely. Bumping updated_at for a no-op is
	// itself a phantom side effect; the activity feed must reflect
	// genuine edits only.
	if len(sets) == 0 && !tagsChanged {
		if err := tx.Commit(); err != nil {
			return domain.Note{}, err
		}
		committed = true
		return previous, nil
	}

	if len(sets) > 0 || tagsChanged {
		// Always stamp updated_at when something changes — including the
		// tag-only path, since tag replacement is a meaningful edit too.
		sets = append([]string{"updated_at = CURRENT_TIMESTAMP"}, sets...)
		args = append(args, id)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE notes SET %s WHERE id = ?`, strings.Join(sets, ", ")), args...); err != nil {
			return domain.Note{}, mapNoteSQLiteError(err)
		}
	}

	if tagsChanged {
		if err := replaceNoteTagsTx(ctx, tx, id, *update.Tags); err != nil {
			return domain.Note{}, err
		}
	}

	refreshed, err := noteByIDTx(ctx, tx, id)
	if err != nil {
		return domain.Note{}, err
	}

	events, err := noteChangeEvents(ctx, s, tx, previous, refreshed, update)
	if err != nil {
		return domain.Note{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Note{}, err
	}
	committed = true
	for _, ev := range events {
		s.publishEvent(ctx, ev)
	}
	return refreshed, nil
}

// noteChangeEvents computes the event slice an UpdateNote call must
// emit by comparing previous vs refreshed (post-UPDATE) snapshots
// against the requested patch. Returns an empty slice when the patch
// resolved to a no-op so the publish loop is naturally empty — caller
// (UpdateNote) short-circuits the UPDATE before reaching here when
// the whole patch is a no-op, but this helper still defends against
// the all-fields-match-previous shape by emitting nothing.
//
// Co-emits note.pinned alongside note.edited when the pinned flag
// actually flipped so the activity feed gets a dedicated toggle line
// without conflating it with arbitrary field edits. The pinned event
// only fires for a real flip, never for a same-value re-write — that
// guard is what TestUpdateNoteSamePinnedDoesNotEmitPinned locks.
func noteChangeEvents(ctx context.Context, s *Store, tx *sql.Tx, previous, refreshed domain.Note, update domain.NoteUpdate) ([]domain.Event, error) {
	scalarChanged := refreshed.Title != previous.Title ||
		refreshed.Body != previous.Body ||
		refreshed.Kind != previous.Kind ||
		refreshed.Pinned != previous.Pinned
	tagsChanged := update.Tags != nil && !sameTagSet(previous.Tags, refreshed.Tags)

	if !scalarChanged && !tagsChanged {
		return nil, nil
	}

	editedEv, err := emitNoteEventTx(ctx, s, tx, refreshed, domain.EventTypeNoteEdited, nil)
	if err != nil {
		return nil, err
	}
	events := []domain.Event{editedEv}

	if update.Pinned != nil && *update.Pinned != previous.Pinned {
		pinnedEv, err := emitNoteEventTx(ctx, s, tx, refreshed, domain.EventTypeNotePinned, map[string]any{
			"pinned": refreshed.Pinned,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, pinnedEv)
	}

	return events, nil
}

// sameTagSet reports whether two tag slices carry the same {name,label}
// pairs, irrespective of order. Used by UpdateNote to suppress phantom
// note.edited emits when a caller re-passes the current tag set.
func sameTagSet(a, b []domain.Tag) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	type key struct{ name, label string }
	counts := make(map[key]int, len(a))
	for _, t := range a {
		counts[key{t.Name, t.Label}]++
	}
	for _, t := range b {
		k := key{t.Name, t.Label}
		counts[k]--
		if counts[k] < 0 {
			return false
		}
	}
	return true
}

// NoteByID returns a single note row plus its tags. Missing rows
// surface a typed ErrValidation so MCP envelopes do not leak driver
// errors.
func (s *Store) NoteByID(ctx context.Context, id int64) (domain.Note, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Note{}, err
	}
	defer func() { _ = tx.Rollback() }()
	note, err := noteByIDTx(ctx, tx, id)
	if err != nil {
		return domain.Note{}, err
	}
	return note, tx.Commit()
}

// ListNotes returns notes matching the filter. Tags are loaded per row
// via a single follow-up query so the result set is fully self-
// describing without an N+1 SELECT loop.
func (s *Store) ListNotes(ctx context.Context, filter domain.NoteFilter) ([]domain.Note, error) {
	var b strings.Builder
	args := []any{}
	b.WriteString(`SELECT id, project_id, kind, title, body, pinned, author_model, created_at, updated_at FROM notes WHERE 1=1`)

	switch filter.Scope {
	case domain.NoteScopeGlobal:
		b.WriteString(` AND project_id IS NULL`)
	case domain.NoteScopeProject:
		if filter.ProjectID <= 0 {
			return nil, domain.NewError(domain.ErrValidation, "scope=project requires project_id", nil)
		}
		b.WriteString(` AND project_id = ?`)
		args = append(args, filter.ProjectID)
	case domain.NoteScopeAny:
		if filter.ProjectID > 0 {
			b.WriteString(` AND project_id = ?`)
			args = append(args, filter.ProjectID)
		}
	}

	if strings.TrimSpace(filter.Kind) != "" {
		b.WriteString(` AND kind = ?`)
		args = append(args, strings.TrimSpace(filter.Kind))
	}
	if filter.Pinned != nil {
		b.WriteString(` AND pinned = ?`)
		args = append(args, boolToInt(*filter.Pinned))
	}
	if len(filter.Tags) > 0 {
		// Match notes that carry EVERY supplied tag (intersection).
		b.WriteString(` AND id IN (SELECT note_id FROM notes_tags nt JOIN tags t ON t.id = nt.tag_id WHERE t.name IN (`)
		b.WriteString(placeholders(len(filter.Tags)))
		b.WriteString(`) GROUP BY note_id HAVING COUNT(DISTINCT t.name) = ?)`)
		for _, t := range filter.Tags {
			args = append(args, t)
		}
		args = append(args, len(filter.Tags))
	}

	b.WriteString(` ORDER BY pinned DESC, updated_at DESC, id DESC`)
	if filter.Limit > 0 {
		b.WriteString(` LIMIT ?`)
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			b.WriteString(` OFFSET ?`)
			args = append(args, filter.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var notes []domain.Note
	for rows.Next() {
		var note domain.Note
		var nullableProject sql.NullInt64
		var nullableAuthor sql.NullString
		if err := rows.Scan(&note.ID, &nullableProject, &note.Kind, &note.Title, &note.Body, &note.Pinned, &nullableAuthor, &note.CreatedAt, &note.UpdatedAt); err != nil {
			return nil, err
		}
		if nullableProject.Valid {
			note.ProjectID = nullableProject.Int64
		}
		if nullableAuthor.Valid {
			note.AuthorModel = nullableAuthor.String
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(notes) == 0 {
		return notes, nil
	}
	ids := make([]int64, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	tagsByNote, err := s.noteTagsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range notes {
		if tags, ok := tagsByNote[notes[i].ID]; ok {
			notes[i].Tags = tags
		}
	}
	return notes, nil
}

// DeleteNote hard-deletes a note row. Tags cascade via FK; the FTS
// triggers from 031 keep notes_fts and search_index consistent.
// The deletion is wrapped in a tx so the note.removed event can be
// recorded in the same atomic block — the title/kind/scope/tags
// snapshot lands in the payload BEFORE the DELETE runs because the
// row (and the notes_tags pivot rows) are gone by the time the audit
// consumer reads back.
func (s *Store) DeleteNote(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	previous, err := noteByIDTx(ctx, tx, id)
	if err != nil {
		return err
	}

	removedEv, err := emitNoteEventTx(ctx, s, tx, previous, domain.EventTypeNoteRemoved, nil)
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// noteByIDTx above already rejected missing rows; reaching this
		// branch implies a concurrent deletion between the SELECT and
		// the DELETE. Surface the same validation error the original
		// implementation returned so MCP envelopes stay consistent.
		return domain.NewError(domain.ErrValidation, "note not found", map[string]any{"note_id": id})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	if removedEv.EventType != "" {
		s.publishEvent(ctx, removedEv)
	}
	return nil
}

// noteByIDTx loads a single note + its tags inside a tx so writers can
// reuse the same connection across the read+write pair.
func noteByIDTx(ctx context.Context, tx *sql.Tx, id int64) (domain.Note, error) {
	var note domain.Note
	var nullableProject sql.NullInt64
	var nullableAuthor sql.NullString
	if err := tx.QueryRowContext(ctx, `
SELECT id, project_id, kind, title, body, pinned, author_model, created_at, updated_at
FROM notes WHERE id = ?`, id).Scan(
		&note.ID, &nullableProject, &note.Kind, &note.Title, &note.Body, &note.Pinned, &nullableAuthor, &note.CreatedAt, &note.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Note{}, domain.NewError(domain.ErrValidation, "note not found", map[string]any{"note_id": id})
		}
		return domain.Note{}, err
	}
	if nullableProject.Valid {
		note.ProjectID = nullableProject.Int64
	}
	if nullableAuthor.Valid {
		note.AuthorModel = nullableAuthor.String
	}

	rows, err := tx.QueryContext(ctx, `
SELECT t.id, t.name, t.label
FROM tags t JOIN notes_tags nt ON nt.tag_id = t.id
WHERE nt.note_id = ? ORDER BY t.name`, id)
	if err != nil {
		return domain.Note{}, err
	}
	defer func() { _ = rows.Close() }()
	var tags []domain.Tag
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Label); err != nil {
			return domain.Note{}, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return domain.Note{}, err
	}
	note.Tags = tags
	return note, nil
}

// replaceNoteTagsTx wipes the notes_tags pivot for the given note and
// re-attaches the supplied tag set in one tx. Centralises the
// "replace all tags for a note" semantic so future writers (UpdateNote,
// any new bulk-tag tool) cannot drift on the DELETE+INSERT sequencing.
// Passing an empty slice clears every tag without reinserting any —
// callers signal "leave alone" upstream by skipping the call entirely.
func replaceNoteTagsTx(ctx context.Context, tx *sql.Tx, noteID int64, tags []domain.Tag) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM notes_tags WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	if _, err := attachNoteTagsTx(ctx, tx, noteID, tags); err != nil {
		return err
	}
	return nil
}

// attachNoteTagsTx mirrors attachTagsTx for the notes_tags pivot.
// Kept separate because notes_tags has no shared owner column with
// event_tags / error_tags so a switch in attachTagsTx would be
// noisier than a dedicated helper.
func attachNoteTagsTx(ctx context.Context, tx *sql.Tx, noteID int64, tags []domain.Tag) ([]domain.Tag, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	out := make([]domain.Tag, 0, len(tags))
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(name, label) VALUES (?, ?)`, tag.Name, tag.Label); err != nil {
			return nil, err
		}
		var stored domain.Tag
		if err := tx.QueryRowContext(ctx, `SELECT id, name, label FROM tags WHERE name = ?`, tag.Name).Scan(&stored.ID, &stored.Name, &stored.Label); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notes_tags(note_id, tag_id) VALUES (?, ?)`, noteID, stored.ID); err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

// noteTagsByIDs loads tags for a batch of note ids in one query so
// ListNotes can hydrate the result set without N+1.
func (s *Store) noteTagsByIDs(ctx context.Context, noteIDs []int64) (map[int64][]domain.Tag, error) {
	if len(noteIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(noteIDs))
	for i, id := range noteIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT nt.note_id, t.id, t.name, t.label
FROM notes_tags nt JOIN tags t ON t.id = nt.tag_id
WHERE nt.note_id IN (`+placeholders(len(noteIDs))+`)
ORDER BY nt.note_id, t.name`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := map[int64][]domain.Tag{}
	for rows.Next() {
		var noteID int64
		var tag domain.Tag
		if err := rows.Scan(&noteID, &tag.ID, &tag.Name, &tag.Label); err != nil {
			return nil, err
		}
		result[noteID] = append(result[noteID], tag)
	}
	return result, rows.Err()
}

// emitNoteEventTx persists a note-scoped event row inside the caller's
// transaction. Composes the canonical {title, kind, scope, tags}
// payload from the supplied note and merges any extra fields the
// caller passes (e.g. note.pinned carries `pinned: <bool>`). Returns
// the persisted row on success, a synthetic envelope when shouldLogEvent
// rejects the type so subscribers still observe the action, or a
// zero-value Event + nil error when both branches no-op (currently
// unreachable — the helper always emits or errors).
//
// The helper centralises the event-shape contract so a future payload
// field reaches every emit site (Create / Update / Delete) through one
// change instead of three drift-prone literals.
func emitNoteEventTx(ctx context.Context, s *Store, tx *sql.Tx, note domain.Note, eventType string, extra map[string]any) (domain.Event, error) {
	payload, err := buildNoteEventPayload(note, extra)
	if err != nil {
		return domain.Event{}, err
	}
	if !s.shouldLogEvent(eventType) {
		return domain.Event{
			EntityType: domain.EventEntityNote,
			EntityID:   note.ID,
			ProjectID:  note.ProjectID,
			EventType:  eventType,
			Payload:    payload,
		}, nil
	}
	return insertEntityEvent(ctx, tx, domain.EventEntityNote, note.ID, note.ProjectID, eventType, payload)
}

// buildNoteEventPayload composes the JSON payload every note.* event
// shares: {title, kind, scope, tags}. Scope is derived from
// note.ProjectID so consumers don't need to inspect the event row's
// project_id column. Extra fields layered on top (without overwriting
// the canonical keys silently — extra wins so note.pinned can carry
// `pinned: <bool>` on top of the standard shape).
func buildNoteEventPayload(note domain.Note, extra map[string]any) (string, error) {
	scope := "project"
	if note.ProjectID == 0 {
		scope = "global"
	}
	tagNames := make([]string, 0, len(note.Tags))
	for _, t := range note.Tags {
		tagNames = append(tagNames, t.Name)
	}
	payload := map[string]any{
		"title": note.Title,
		"kind":  note.Kind,
		"scope": scope,
		"tags":  tagNames,
	}
	for k, v := range extra {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal note event payload: %w", err)
	}
	return string(body), nil
}

func nullStringIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// mapNoteSQLiteError surfaces FK violations on the notes path as typed
// validation_error responses so MCP callers see a recoverable shape
// instead of the raw driver message.
func mapNoteSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "FOREIGN KEY constraint failed") {
		return domain.NewError(domain.ErrValidation, "referenced row does not exist", map[string]any{"hint": "project_id must reference an existing project"})
	}
	return err
}

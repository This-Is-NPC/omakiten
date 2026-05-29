package sqlite

import (
	"context"
	"database/sql"
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
	defer func() { _ = tx.Rollback() }()

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

	if err := tx.Commit(); err != nil {
		return domain.Note{}, err
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
	defer func() { _ = tx.Rollback() }()

	if _, err := noteByIDTx(ctx, tx, id); err != nil {
		return domain.Note{}, err
	}

	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	if update.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *update.Title)
	}
	if update.Body != nil {
		sets = append(sets, "body = ?")
		args = append(args, *update.Body)
	}
	if update.Kind != nil {
		sets = append(sets, "kind = ?")
		args = append(args, *update.Kind)
	}
	if update.Pinned != nil {
		sets = append(sets, "pinned = ?")
		args = append(args, boolToInt(*update.Pinned))
	}

	// Always run the UPDATE (even when only tags change) so updated_at
	// reflects the patch — tag replacement is a meaningful edit too.
	args = append(args, id)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE notes SET %s WHERE id = ?`, strings.Join(sets, ", ")), args...); err != nil {
		return domain.Note{}, mapNoteSQLiteError(err)
	}

	if update.Tags != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notes_tags WHERE note_id = ?`, id); err != nil {
			return domain.Note{}, err
		}
		domainTags := make([]domain.Tag, 0, len(*update.Tags))
		for _, raw := range *update.Tags {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			domainTags = append(domainTags, domain.Tag{Name: name, Label: name})
		}
		if _, err := attachNoteTagsTx(ctx, tx, id, domainTags); err != nil {
			return domain.Note{}, err
		}
	}

	refreshed, err := noteByIDTx(ctx, tx, id)
	if err != nil {
		return domain.Note{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Note{}, err
	}
	return refreshed, nil
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
func (s *Store) DeleteNote(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.NewError(domain.ErrValidation, "note not found", map[string]any{"note_id": id})
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

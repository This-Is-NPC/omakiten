package syncserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"omakiten/internal/domain"
	"omakiten/internal/syncproto"
)

//go:embed schema.sql
var schema string

// Store keeps one row per (project, table, row_uid) in its own SQLite file.
// Conflicts resolve per row. Entity rows: a delete replaces a stored upsert
// whatever its HLC, a stored delete is final and refuses every later change,
// and an upsert replaces a stored upsert only with a greater HLC. Join rows
// (domain.SyncDeleteIsFinal is false) follow plain last-writer-wins on the HLC for upserts and
// deletes alike, so a removed link can be added again.
type Store struct {
	db *sql.DB
}

// Open opens or creates the store file and its schema. A new file is created
// owner-only (0600); SQLite gives its -wal and -shm files the same mode.
func Open(ctx context.Context, path string) (*Store, error) {
	query := url.Values{
		"_pragma": {"journal_mode(WAL)", "busy_timeout(5000)", "foreign_keys(1)"},
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open sync store: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("open sync store: %w", err)
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: query.Encode()}).String())
	if err != nil {
		return nil, fmt.Errorf("open sync store: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create sync store schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Projects lists the projects held, ordered by slug.
func (s *Store) Projects(ctx context.Context) ([]domain.SyncProjectInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uid, name, slug, description, kit_key FROM projects ORDER BY slug, uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	infos := []domain.SyncProjectInfo{}
	for rows.Next() {
		var info domain.SyncProjectInfo
		if err := rows.Scan(&info.UID, &info.Name, &info.Slug, &info.Description, &info.KitKey); err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, rows.Err()
}

// Push upserts the project and stores every change that wins against the
// stored row, giving each a new seq. An invalid project fails the request; a
// change invalid at now is skipped and counted as rejected. It returns the
// accepted and rejected counts and the project's latest seq.
func (s *Store) Push(ctx context.Context, req syncproto.PushRequest, now time.Time) (syncproto.PushResponse, error) {
	if err := domain.ValidateSyncProjectInfo(req.Project); err != nil {
		return syncproto.PushResponse{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return syncproto.PushResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	info := req.Project
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO projects (uid, name, slug, description, kit_key) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(uid) DO UPDATE SET name = excluded.name, slug = excluded.slug,
			description = excluded.description, kit_key = excluded.kit_key`,
		info.UID, info.Name, info.Slug, info.Description, info.KitKey); err != nil {
		return syncproto.PushResponse{}, err
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM rows`).Scan(&seq); err != nil {
		return syncproto.PushResponse{}, err
	}
	accepted, rejected := 0, 0
	for _, c := range req.Changes {
		if err := domain.ValidateSyncChange(c, now); err != nil {
			rejected++
			continue
		}
		won, err := wins(ctx, tx, info.UID, c)
		if err != nil {
			return syncproto.PushResponse{}, err
		}
		if !won {
			continue
		}
		seq++
		if err := put(ctx, tx, info.UID, c, seq); err != nil {
			return syncproto.PushResponse{}, err
		}
		accepted++
	}
	var cursor int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM rows WHERE project_uid = ?`, info.UID).Scan(&cursor); err != nil {
		return syncproto.PushResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return syncproto.PushResponse{}, err
	}
	return syncproto.PushResponse{Accepted: accepted, Rejected: rejected, Cursor: cursor}, nil
}

// put writes c as the row's current version under seq.
func put(ctx context.Context, tx *sql.Tx, project domain.UID, c domain.SyncChange, seq int64) error {
	data, err := json.Marshal(c.Data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO rows (project_uid, tbl, row_uid, op, hlc, device, data, seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_uid, tbl, row_uid) DO UPDATE SET op = excluded.op, hlc = excluded.hlc,
			device = excluded.device, data = excluded.data, seq = excluded.seq`,
		project, c.Table, c.RowUID, c.Op, c.HLC, c.Device, string(data), seq)
	return err
}

// wins reports whether c replaces the stored row under the Store conflict rule.
func wins(ctx context.Context, tx *sql.Tx, project domain.UID, c domain.SyncChange) (bool, error) {
	var op, stored string
	err := tx.QueryRowContext(ctx, `SELECT op, hlc FROM rows WHERE project_uid = ? AND tbl = ? AND row_uid = ?`,
		project, c.Table, c.RowUID).Scan(&op, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if domain.SyncDeleteIsFinal(c.Table) {
		switch {
		case domain.SyncOp(op) == domain.SyncOpDelete:
			return false, nil
		case c.Op == domain.SyncOpDelete:
			return true, nil
		}
	}
	have, err := domain.ParseHLC(stored)
	if err != nil {
		return false, err
	}
	incoming, err := domain.ParseHLC(c.HLC)
	if err != nil {
		return false, err
	}
	return incoming.Compare(have) > 0, nil
}

// Pull returns up to limit changes of the project with seq above after, in seq order.
func (s *Store) Pull(ctx context.Context, project domain.UID, after int64, limit int) (syncproto.PullResponse, error) {
	if limit <= 0 {
		limit = syncproto.DefaultPullLimit
	}
	limit = min(limit, syncproto.MaxPullLimit)
	resp := syncproto.PullResponse{Changes: []domain.SyncChange{}, Cursor: after}
	err := s.db.QueryRowContext(ctx, `SELECT uid, name, slug, description, kit_key FROM projects WHERE uid = ?`, project).
		Scan(&resp.Project.UID, &resp.Project.Name, &resp.Project.Slug, &resp.Project.Description, &resp.Project.KitKey)
	if errors.Is(err, sql.ErrNoRows) {
		return resp, domain.NewError(domain.ErrProjectNotFound, "project "+string(project)+" is not on this server",
			map[string]any{"uid": string(project)})
	}
	if err != nil {
		return resp, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tbl, row_uid, op, hlc, device, data, seq FROM rows
		WHERE project_uid = ? AND seq > ? ORDER BY seq LIMIT ?`, project, after, limit+1)
	if err != nil {
		return resp, err
	}
	defer rows.Close()
	var last int64
	for rows.Next() {
		var c domain.SyncChange
		var data string
		var seq int64
		if err := rows.Scan(&c.Table, &c.RowUID, &c.Op, &c.HLC, &c.Device, &data, &seq); err != nil {
			return resp, err
		}
		if len(resp.Changes) == limit {
			resp.More = true
			break
		}
		if err := json.Unmarshal([]byte(data), &c.Data); err != nil {
			return resp, err
		}
		resp.Changes = append(resp.Changes, c)
		last = seq
	}
	if err := rows.Err(); err != nil {
		return resp, err
	}
	if len(resp.Changes) > 0 {
		resp.Cursor = last
	}
	return resp, nil
}

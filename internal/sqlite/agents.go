package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
)

const agentSessionColumns = `id, team_id, member, command, persona, harness, model, launcher, pane, status, error, created_at, COALESCE(finished_at, '')`

// OpenAgentTeam records team as the active team of its task in its bucket.
// When the task already has an active team there, it returns that team and
// false instead.
func (s *Store) OpenAgentTeam(ctx context.Context, team domain.AgentTeam) (domain.AgentTeam, bool, error) {
	tx, err := s.beginTransaction(ctx)
	if err != nil {
		return domain.AgentTeam{}, false, err
	}
	defer func() { _ = s.rollbackTransaction(ctx, tx) }()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM agent_teams WHERE project_id = ? AND task_id = ? AND bucket_key = ? AND status = 'active'`, team.ProjectID, team.TaskID, team.BucketKey).Scan(&existing)
	if err == nil {
		if err := s.commitTransaction(ctx, tx); err != nil {
			return domain.AgentTeam{}, false, err
		}
		found, err := s.GetAgentTeam(ctx, existing)
		return found, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.AgentTeam{}, false, err
	}
	err = tx.QueryRowContext(ctx, `
INSERT INTO agent_teams(id, project_id, task_id, bucket_key, status)
VALUES (?, ?, ?, ?, 'active')
RETURNING status, created_at`, team.ID, team.ProjectID, team.TaskID, team.BucketKey).Scan(&team.Status, &team.CreatedAt)
	if err != nil {
		return domain.AgentTeam{}, false, err
	}
	return team, true, s.commitTransaction(ctx, tx)
}

// FailAgentTeam marks a team failed, which frees its task and bucket.
func (s *Store) FailAgentTeam(ctx context.Context, teamID string) error {
	_, err := s.query(ctx).ExecContext(ctx, `UPDATE agent_teams SET status = 'failed' WHERE id = ?`, teamID)
	return err
}

// CloseAgentTeam closes an active team and stops its running sessions. It
// reports whether the team was active.
func (s *Store) CloseAgentTeam(ctx context.Context, teamID string) (bool, error) {
	tx, err := s.beginTransaction(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = s.rollbackTransaction(ctx, tx) }()
	res, err := tx.ExecContext(ctx, `UPDATE agent_teams SET status = 'closed' WHERE id = ? AND status = 'active'`, teamID)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_sessions SET status = 'stopped', finished_at = CURRENT_TIMESTAMP WHERE team_id = ? AND status = 'spawned'`, teamID); err != nil {
		return false, err
	}
	return true, s.commitTransaction(ctx, tx)
}

// AddAgentSession records a session of a team.
func (s *Store) AddAgentSession(ctx context.Context, session domain.AgentSession) error {
	_, err := s.query(ctx).ExecContext(ctx, `
INSERT INTO agent_sessions(id, team_id, member, command, persona, harness, model, launcher, pane, status, error, finished_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CASE WHEN ? = 'spawned' THEN NULL ELSE CURRENT_TIMESTAMP END)`,
		session.ID, session.TeamID, session.Member, session.Command, session.Persona, session.Harness, session.Model, session.Launcher, session.Pane, session.Status, session.Error, session.Status)
	return err
}

// FinishAgentSession marks a spawned session done. It reports whether the
// session was still spawned.
func (s *Store) FinishAgentSession(ctx context.Context, sessionID string) (bool, error) {
	res, err := s.query(ctx).ExecContext(ctx, `UPDATE agent_sessions SET status = 'done', finished_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'spawned'`, sessionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReplaceAgentSession marks a session replaced by a newer one for its seat.
func (s *Store) ReplaceAgentSession(ctx context.Context, sessionID string) error {
	_, err := s.query(ctx).ExecContext(ctx, `UPDATE agent_sessions SET status = 'replaced', finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP) WHERE id = ? AND status != 'replaced'`, sessionID)
	return err
}

// GetAgentTeam returns a team with its sessions in creation order.
func (s *Store) GetAgentTeam(ctx context.Context, teamID string) (domain.AgentTeam, error) {
	var team domain.AgentTeam
	err := s.query(ctx).QueryRowContext(ctx, `SELECT id, project_id, task_id, bucket_key, status, created_at FROM agent_teams WHERE id = ?`, teamID).
		Scan(&team.ID, &team.ProjectID, &team.TaskID, &team.BucketKey, &team.Status, &team.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentTeam{}, domain.NewError(domain.ErrAgentNotFound, "agent team not found", map[string]any{"team_id": teamID})
	}
	if err != nil {
		return domain.AgentTeam{}, err
	}
	rows, err := s.query(ctx).QueryContext(ctx, `SELECT `+agentSessionColumns+` FROM agent_sessions WHERE team_id = ? ORDER BY rowid`, teamID)
	if err != nil {
		return domain.AgentTeam{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		session, err := scanAgentSession(rows)
		if err != nil {
			return domain.AgentTeam{}, err
		}
		team.Sessions = append(team.Sessions, session)
	}
	return team, rows.Err()
}

// GetAgentSession returns one session.
func (s *Store) GetAgentSession(ctx context.Context, sessionID string) (domain.AgentSession, error) {
	session, err := scanAgentSession(s.query(ctx).QueryRowContext(ctx, `SELECT `+agentSessionColumns+` FROM agent_sessions WHERE id = ?`, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentSession{}, domain.NewError(domain.ErrAgentNotFound, "agent session not found", map[string]any{"session_id": sessionID})
	}
	return session, err
}

// ActiveAgentTeamIDs returns the ids of the task's active teams.
func (s *Store) ActiveAgentTeamIDs(ctx context.Context, projectID, taskID int64) ([]string, error) {
	rows, err := s.query(ctx).QueryContext(ctx, `SELECT id FROM agent_teams WHERE project_id = ? AND task_id = ? AND status = 'active' ORDER BY rowid`, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CountSpawnedAgentSessions counts the project's sessions still running in
// active teams.
func (s *Store) CountSpawnedAgentSessions(ctx context.Context, projectID int64) (int, error) {
	var n int
	err := s.query(ctx).QueryRowContext(ctx, `
SELECT COUNT(*) FROM agent_sessions s JOIN agent_teams t ON t.id = s.team_id
WHERE t.project_id = ? AND t.status = 'active' AND s.status = 'spawned'`, projectID).Scan(&n)
	return n, err
}

// LastAgentActivity returns the newest event recorded by a session, or a
// zero row when it recorded none.
func (s *Store) LastAgentActivity(ctx context.Context, sessionID string) (domain.EventRow, error) {
	var row domain.EventRow
	err := s.query(ctx).QueryRowContext(ctx, `
SELECT id, event_type, created_at FROM events
WHERE agent_session_id = ? ORDER BY id DESC LIMIT 1`, sessionID).Scan(&row.ID, &row.EventType, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EventRow{}, nil
	}
	return row, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgentSession(row rowScanner) (domain.AgentSession, error) {
	var s domain.AgentSession
	err := row.Scan(&s.ID, &s.TeamID, &s.Member, &s.Command, &s.Persona, &s.Harness, &s.Model, &s.Launcher, &s.Pane, &s.Status, &s.Error, &s.CreatedAt, &s.FinishedAt)
	return s, err
}

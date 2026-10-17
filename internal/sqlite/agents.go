package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"omakiten/internal/domain"
)

// agentSessionColumns selects a session's columns, each qualified by
// prefix when one is given.
func agentSessionColumns(prefix string) string {
	return fmt.Sprintf(`%[1]sid, %[1]steam_id, %[1]smember, %[1]scommand, %[1]spersona, %[1]sharness, %[1]smodel, %[1]spid, %[1]stranscript, %[1]sstatus, %[1]sexit_code, %[1]serror, %[1]screated_at, COALESCE(%[1]sfinished_at, '')`, prefix)
}

// agentTeamColumns selects a team's columns, a missing task or plan as 0.
const agentTeamColumns = `id, project_id, COALESCE(task_id, 0), COALESCE(plan_id, 0), bucket_key, status, opened_after, created_at`

func scanAgentTeam(row rowScanner) (domain.AgentTeam, error) {
	var team domain.AgentTeam
	err := row.Scan(&team.ID, &team.ProjectID, &team.TaskID, &team.PlanID, &team.BucketKey, &team.Status, &team.OpenedAfter, &team.CreatedAt)
	return team, err
}

// OpenAgentTeam records team as the active team of its task in its bucket,
// or of its plan when it names one. When one is already active there, it
// returns that team and false instead. The insert comes first, so
// concurrent opens wait for each other's write.
func (s *Store) OpenAgentTeam(ctx context.Context, team domain.AgentTeam) (domain.AgentTeam, bool, error) {
	tx, err := s.beginTransaction(ctx)
	if err != nil {
		return domain.AgentTeam{}, false, err
	}
	defer func() { _ = s.rollbackTransaction(ctx, tx) }()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_teams(id, project_id, task_id, plan_id, bucket_key, status, opened_after)
VALUES (?, ?, NULLIF(?, 0), NULLIF(?, 0), ?, 'active', (SELECT COALESCE(MAX(id), 0) FROM events))
ON CONFLICT DO NOTHING`, team.ID, team.ProjectID, team.TaskID, team.PlanID, team.BucketKey); err != nil {
		return domain.AgentTeam{}, false, err
	}
	var active string
	if err := tx.QueryRowContext(ctx, `
SELECT id FROM agent_teams WHERE project_id = ? AND COALESCE(task_id, 0) = ? AND COALESCE(plan_id, 0) = ? AND bucket_key = ? AND status = 'active'`,
		team.ProjectID, team.TaskID, team.PlanID, team.BucketKey).Scan(&active); err != nil {
		return domain.AgentTeam{}, false, err
	}
	if err := s.commitTransaction(ctx, tx); err != nil {
		return domain.AgentTeam{}, false, err
	}
	found, err := s.GetAgentTeam(ctx, active)
	return found, active == team.ID, err
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
	if _, err := tx.ExecContext(ctx, `UPDATE agent_sessions SET status = 'stopped', finished_at = CURRENT_TIMESTAMP WHERE team_id = ? AND status = 'running'`, teamID); err != nil {
		return false, err
	}
	return true, s.commitTransaction(ctx, tx)
}

// ReserveAgentSessions records sessions as running in their teams, after
// marking the session replaced names replaced when it is set, as one
// write. With a positive limit it refuses, recording nothing, when the
// project would then run more than limit sessions in active teams.
func (s *Store) ReserveAgentSessions(ctx context.Context, projectID int64, sessions []domain.AgentSession, replaced string, limit int) error {
	tx, err := s.beginTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = s.rollbackTransaction(ctx, tx) }()
	if replaced != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE agent_sessions SET status = 'replaced', finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP) WHERE id = ? AND status != 'replaced'`, replaced); err != nil {
			return err
		}
	}
	for _, session := range sessions {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_sessions(id, team_id, member, command, persona, harness, model, transcript, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'running')`,
			session.ID, session.TeamID, session.Member, session.Command, session.Persona, session.Harness, session.Model, session.Transcript); err != nil {
			return err
		}
	}
	if limit > 0 {
		var running int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM agent_sessions s JOIN agent_teams t ON t.id = s.team_id
WHERE t.project_id = ? AND t.status = 'active' AND s.status = 'running'`, projectID).Scan(&running); err != nil {
			return err
		}
		if running > limit {
			return domain.SessionLimitError{Running: running - len(sessions), Limit: limit}
		}
	}
	return s.commitTransaction(ctx, tx)
}

// SetAgentSessionPID records the pid of a session's supervisor.
func (s *Store) SetAgentSessionPID(ctx context.Context, sessionID string, pid int) error {
	_, err := s.query(ctx).ExecContext(ctx, `UPDATE agent_sessions SET pid = ? WHERE id = ?`, pid, sessionID)
	return err
}

// FinishAgentSession records how a running session ended. It reports
// whether the session was still running, and whether its team is active
// with no session left running.
func (s *Store) FinishAgentSession(ctx context.Context, sessionID, status string, exitCode *int, message string) (finished, idle bool, err error) {
	tx, err := s.beginTransaction(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = s.rollbackTransaction(ctx, tx) }()
	res, err := tx.ExecContext(ctx, `
UPDATE agent_sessions SET status = ?, exit_code = ?, error = ?, finished_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = 'running'`, status, exitCode, message, sessionID)
	if err != nil {
		return false, false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, false, err
	}
	err = tx.QueryRowContext(ctx, `
SELECT t.status = 'active' AND NOT EXISTS (SELECT 1 FROM agent_sessions r WHERE r.team_id = t.id AND r.status = 'running')
FROM agent_teams t JOIN agent_sessions s ON s.team_id = t.id WHERE s.id = ?`, sessionID).Scan(&idle)
	if err != nil {
		return false, false, err
	}
	return true, idle, s.commitTransaction(ctx, tx)
}

// GetAgentTeam returns a team with its sessions in creation order.
func (s *Store) GetAgentTeam(ctx context.Context, teamID string) (domain.AgentTeam, error) {
	team, err := scanAgentTeam(s.query(ctx).QueryRowContext(ctx, `SELECT `+agentTeamColumns+` FROM agent_teams WHERE id = ?`, teamID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentTeam{}, domain.NewError(domain.ErrAgentNotFound, "agent team not found", map[string]any{"team_id": teamID})
	}
	if err != nil {
		return domain.AgentTeam{}, err
	}
	rows, err := s.query(ctx).QueryContext(ctx, `SELECT `+agentSessionColumns("")+` FROM agent_sessions WHERE team_id = ? ORDER BY rowid`, teamID)
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

const agentTeamListLimit = 200

// ListAgentTeams returns the project's teams newest first, each with its
// sessions, capped at agentTeamListLimit.
func (s *Store) ListAgentTeams(ctx context.Context, projectID int64, filter domain.AgentTeamFilter) ([]domain.AgentTeam, error) {
	query := `SELECT ` + agentTeamColumns + ` FROM agent_teams WHERE project_id = ?`
	args := []any{projectID}
	if filter.TaskID > 0 {
		query += ` AND task_id = ?`
		args = append(args, filter.TaskID)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY rowid DESC LIMIT ?`
	args = append(args, agentTeamListLimit)
	rows, err := s.query(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var teams []domain.AgentTeam
	index := map[string]int{}
	for rows.Next() {
		team, err := scanAgentTeam(rows)
		if err != nil {
			return nil, err
		}
		index[team.ID] = len(teams)
		teams = append(teams, team)
	}
	if err := rows.Err(); err != nil || len(teams) == 0 {
		return teams, err
	}
	return teams, s.attachAgentSessions(ctx, teams, index)
}

// attachAgentSessions loads the sessions of every team in one query.
func (s *Store) attachAgentSessions(ctx context.Context, teams []domain.AgentTeam, index map[string]int) error {
	args := make([]any, len(teams))
	for i, team := range teams {
		args[i] = team.ID
	}
	rows, err := s.query(ctx).QueryContext(ctx, `SELECT `+agentSessionColumns("")+` FROM agent_sessions WHERE team_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`) ORDER BY rowid`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		session, err := scanAgentSession(rows)
		if err != nil {
			return err
		}
		i := index[session.TeamID]
		teams[i].Sessions = append(teams[i].Sessions, session)
	}
	return rows.Err()
}

// GetAgentSession returns one session.
func (s *Store) GetAgentSession(ctx context.Context, sessionID string) (domain.AgentSession, error) {
	session, err := scanAgentSession(s.query(ctx).QueryRowContext(ctx, `SELECT `+agentSessionColumns("")+` FROM agent_sessions WHERE id = ?`, sessionID))
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

// StaleRunningAgentSessions returns the project's sessions that have been
// running for longer than age, each with its team.
func (s *Store) StaleRunningAgentSessions(ctx context.Context, projectID int64, age time.Duration) ([]domain.TeamSession, error) {
	rows, err := s.query(ctx).QueryContext(ctx, `
SELECT t.id, t.project_id, COALESCE(t.task_id, 0), COALESCE(t.plan_id, 0), t.bucket_key, `+agentSessionColumns("s.")+`
FROM agent_sessions s JOIN agent_teams t ON t.id = s.team_id
WHERE t.project_id = ? AND s.status = 'running' AND s.created_at < datetime('now', ?)
ORDER BY s.rowid`, projectID, fmt.Sprintf("-%d seconds", int(age.Seconds())))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.TeamSession
	for rows.Next() {
		var ts domain.TeamSession
		session, err := scanAgentSession(rows, &ts.Team.ID, &ts.Team.ProjectID, &ts.Team.TaskID, &ts.Team.PlanID, &ts.Team.BucketKey)
		if err != nil {
			return nil, err
		}
		ts.Session = session
		out = append(out, ts)
	}
	return out, rows.Err()
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

// scanAgentSession scans the leading columns into lead, then the session.
func scanAgentSession(row rowScanner, lead ...any) (domain.AgentSession, error) {
	var s domain.AgentSession
	var exitCode sql.NullInt64
	err := row.Scan(append(lead, &s.ID, &s.TeamID, &s.Member, &s.Command, &s.Persona, &s.Harness, &s.Model, &s.PID, &s.Transcript, &s.Status, &exitCode, &s.Error, &s.CreatedAt, &s.FinishedAt)...)
	if exitCode.Valid {
		code := int(exitCode.Int64)
		s.ExitCode = &code
	}
	return s, err
}

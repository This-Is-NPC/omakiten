package contract

import "omakiten/internal/domain"

// SpawnAgentsInput opens agents for a task. Without Command it opens the
// team the task's bucket declares; Command opens a team of one lead. TeamID
// with Member instead reopens that roster seat of an active team. The other
// fields override the opened seat's adapters.
type SpawnAgentsInput struct {
	ProjectSelector
	TaskID   int64  `json:"task_id,omitempty"`
	TeamID   string `json:"team_id,omitempty"`
	Member   string `json:"member,omitempty"`
	Command  string `json:"command,omitempty"`
	Harness  string `json:"harness,omitempty"`
	Model    string `json:"model,omitempty"`
	Launcher string `json:"launcher,omitempty"`
}

type SpawnAgentsResponse struct {
	Project ProjectSummary     `json:"project"`
	Team    domain.TeamOutcome `json:"team"`
}

type ShowAgentInput struct {
	ProjectSelector
	SessionID string `json:"session_id"`
}

// AgentActivity is the newest event a session recorded.
type AgentActivity struct {
	EventID   int64  `json:"event_id"`
	EventType string `json:"event_type"`
	CreatedAt string `json:"created_at"`
}

type ShowAgentResponse struct {
	Project      ProjectSummary      `json:"project"`
	Session      domain.AgentSession `json:"session"`
	TaskID       int64               `json:"task_id"`
	Bucket       string              `json:"bucket"`
	LastActivity *AgentActivity      `json:"last_activity,omitempty"`
	Follow       []string            `json:"follow"`
}

type ShowTeamInput struct {
	ProjectSelector
	TeamID string `json:"team_id"`
}

// TeamResult is the review-<key> comment a member published.
type TeamResult struct {
	Member    string `json:"member"`
	CommentID int64  `json:"comment_id"`
	CreatedAt string `json:"created_at"`
}

type ShowTeamResponse struct {
	Project ProjectSummary   `json:"project"`
	Team    domain.AgentTeam `json:"team"`
	Results []TeamResult     `json:"results"`
	Follow  []string         `json:"follow"`
}

type CloseTeamInput struct {
	ProjectSelector
	TeamID string `json:"team_id"`
}

type CloseTeamResponse struct {
	Project ProjectSummary   `json:"project"`
	Team    domain.AgentTeam `json:"team"`
}

// ListAgentsInput filters agent.list by task and team status (active,
// failed, closed); zero fields do not filter.
type ListAgentsInput struct {
	ProjectSelector
	TaskID int64  `json:"task_id,omitempty"`
	Status string `json:"status,omitempty"`
}

type ListAgentsResponse struct {
	Project ProjectSummary     `json:"project"`
	Teams   []domain.AgentTeam `json:"teams"`
}

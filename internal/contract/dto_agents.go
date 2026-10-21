package contract

import (
	"time"

	"omakiten/internal/domain"
)

// SpawnAgentsInput starts agents for a task. Without Command it starts the
// team the task's bucket declares; Command starts a team of one lead.
// TeamID with Member instead reruns that roster seat of an active team,
// and Plan, a plan slug, with Member reruns that plan seat. Harness and
// Model override the started seat's.
type SpawnAgentsInput struct {
	ProjectSelector
	TaskID  int64  `json:"task_id,omitempty"`
	TeamID  string `json:"team_id,omitempty"`
	Plan    string `json:"plan,omitempty"`
	Member  string `json:"member,omitempty"`
	Command string `json:"command,omitempty"`
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
}

// SpawnSeatInput starts one seat: a roster seat of the team TeamID names,
// or of the active team of TaskID, or the plan seat of PlanID.
type SpawnSeatInput struct {
	ProjectSelector
	TeamID string `json:"team_id,omitempty"`
	TaskID int64  `json:"task_id,omitempty"`
	PlanID int64  `json:"plan_id,omitempty"`
	Member string `json:"member"`
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
	TaskID       int64               `json:"task_id,omitempty"`
	PlanID       int64               `json:"plan_id,omitempty"`
	Bucket       string              `json:"bucket,omitempty"`
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

// ShowTeamResponse is a team with the results its members published.
// MissingResults names the members that finished done without one.
type ShowTeamResponse struct {
	Project        ProjectSummary   `json:"project"`
	Team           domain.AgentTeam `json:"team"`
	Results        []TeamResult     `json:"results"`
	MissingResults []string         `json:"missing_results"`
	Follow         []string         `json:"follow"`
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

// RunAgentSessionInput is the supervisor's run of one session: the harness
// argv and the timeout that ends it, zero for none.
type RunAgentSessionInput struct {
	ProjectSelector
	SessionID string        `json:"session_id"`
	Argv      []string      `json:"argv"`
	Timeout   time.Duration `json:"timeout,omitempty"`
}

type RunAgentSessionResponse struct {
	Project ProjectSummary      `json:"project"`
	Session domain.AgentSession `json:"session"`
}

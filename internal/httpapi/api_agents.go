package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

// AgentOperations opens agent teams for tasks and reads their sessions.
type AgentOperations interface {
	SpawnAgents(ctx context.Context, input contract.SpawnAgentsInput) (contract.SpawnAgentsResponse, error)
	ShowAgent(ctx context.Context, input contract.ShowAgentInput) (contract.ShowAgentResponse, error)
	ListAgents(ctx context.Context, input contract.ListAgentsInput) (contract.ListAgentsResponse, error)
	ShowTeam(ctx context.Context, input contract.ShowTeamInput) (contract.ShowTeamResponse, error)
	CloseTeam(ctx context.Context, input contract.CloseTeamInput) (contract.CloseTeamResponse, error)
	Wait(ctx context.Context, input contract.WaitInput) (contract.WaitResponse, error)
}

// SpawnBody is the spawnAgents request body; empty fields keep the
// bucket's declared team and adapters.
type SpawnBody struct {
	Command string `json:"command,omitempty"`
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
}

func (s *Server) agentRoutes() []route {
	return []route{
		command("spawnAgents", http.MethodPost, taskPath+"/agents", "agent.spawn", "Start the bucket's agent team, or one agent running a command, for a task.", []param{projectParam, taskParam}, s.spawnAgents),
		command("respawnTeamMember", http.MethodPost, projectPath+"/teams/{team}/members/{member}", "agent.spawn", "Rerun one roster seat of an active agent team in a new session.", []param{
			projectParam,
			pathParam("team", "Agent team id.", stringSchema),
			pathParam("member", "Roster key of the seat; `lead` for the lead.", stringSchema),
		}, s.respawnMember),
		query("listAgents", http.MethodGet, projectPath+"/agents", "agent.list", "A project's agent teams with their sessions, newest first.", []param{
			projectParam,
			queryParam("task", "Task id whose teams to keep.", intSchema),
			queryParam("status", "Team status: `active`, `failed`, or `closed`.", stringSchema),
		}, s.listAgents),
		query("getAgent", http.MethodGet, projectPath+"/agents/{session}", "agent.show", "An agent session and its last activity.", []param{
			projectParam,
			pathParam("session", "Agent session id.", stringSchema),
		}, s.showAgent),
		query("getTeam", http.MethodGet, projectPath+"/teams/{team}", "team.show", "An agent team, its sessions, and the results they published.", []param{
			projectParam,
			pathParam("team", "Agent team id.", stringSchema),
		}, s.showTeam),
		query("closeTeam", http.MethodPost, projectPath+"/teams/{team}/close", "team.close", "Close an agent team and stop its running seats.", []param{
			projectParam,
			pathParam("team", "Agent team id.", stringSchema),
		}, s.closeTeam),
		query("waitTaskComment", http.MethodGet, taskPath+"/wait", "wait", "Block until a comment of the task carries a matching tag.", []param{
			projectParam,
			taskParam,
			queryParam("tag", "Tag glob such as `review-*`.", stringSchema),
			queryParam("timeout", "Go duration up to `1h`; default `10m`.", stringSchema),
			queryParam("after", "Event id to wait after; default now.", intSchema),
		}, s.wait),
	}
}

func (s *Server) wait(r *http.Request) (contract.WaitResponse, error) {
	after, err := optionalInt(r, "after")
	if err != nil {
		return contract.WaitResponse{}, err
	}
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.WaitResponse{}, err
	}
	return ops.Wait(r.Context(), contract.WaitInput{ProjectSelector: selector, TaskID: taskID, Tag: r.URL.Query().Get("tag"), Timeout: r.URL.Query().Get("timeout"), AfterID: int64(after)})
}

func (s *Server) spawnAgents(r *http.Request, body SpawnBody) (contract.SpawnAgentsResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	return ops.SpawnAgents(r.Context(), contract.SpawnAgentsInput{ProjectSelector: selector, TaskID: taskID, Command: body.Command, Harness: body.Harness, Model: body.Model})
}

func (s *Server) respawnMember(r *http.Request, body SpawnBody) (contract.SpawnAgentsResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	return ops.SpawnAgents(r.Context(), contract.SpawnAgentsInput{ProjectSelector: selector, TeamID: r.PathValue("team"), Member: r.PathValue("member"), Harness: body.Harness, Model: body.Model})
}

func (s *Server) listAgents(r *http.Request) (contract.ListAgentsResponse, error) {
	task, err := optionalInt(r, "task")
	if err != nil {
		return contract.ListAgentsResponse{}, err
	}
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ListAgentsResponse{}, err
	}
	return ops.ListAgents(r.Context(), contract.ListAgentsInput{ProjectSelector: selector, TaskID: int64(task), Status: r.URL.Query().Get("status")})
}

func (s *Server) showAgent(r *http.Request) (contract.ShowAgentResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ShowAgentResponse{}, err
	}
	return ops.ShowAgent(r.Context(), contract.ShowAgentInput{ProjectSelector: selector, SessionID: r.PathValue("session")})
}

func (s *Server) closeTeam(r *http.Request) (contract.CloseTeamResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.CloseTeamResponse{}, err
	}
	return ops.CloseTeam(r.Context(), contract.CloseTeamInput{ProjectSelector: selector, TeamID: r.PathValue("team")})
}

func (s *Server) showTeam(r *http.Request) (contract.ShowTeamResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	return ops.ShowTeam(r.Context(), contract.ShowTeamInput{ProjectSelector: selector, TeamID: r.PathValue("team")})
}

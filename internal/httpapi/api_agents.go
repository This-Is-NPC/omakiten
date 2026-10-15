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
	ShowTeam(ctx context.Context, input contract.ShowTeamInput) (contract.ShowTeamResponse, error)
}

// SpawnBody is the spawnAgents request body; empty fields keep the
// bucket's declared team and adapters.
type SpawnBody struct {
	Command  string `json:"command,omitempty"`
	Harness  string `json:"harness,omitempty"`
	Model    string `json:"model,omitempty"`
	Launcher string `json:"launcher,omitempty"`
}

func (s *Server) agentRoutes() []route {
	return []route{
		command("spawnAgents", http.MethodPost, taskPath+"/agents", "agent.spawn", "Open the bucket's agent team, or one agent running a command, for a task.", []param{projectParam, taskParam}, s.spawnAgents),
		query("getAgent", http.MethodGet, projectPath+"/agents/{session}", "agent.show", "An agent session and its last activity.", []param{
			projectParam,
			pathParam("session", "Agent session id.", stringSchema),
		}, s.showAgent),
		query("getTeam", http.MethodGet, projectPath+"/teams/{team}", "team.show", "An agent team, its sessions, and the results they published.", []param{
			projectParam,
			pathParam("team", "Agent team id.", stringSchema),
		}, s.showTeam),
	}
}

func (s *Server) spawnAgents(r *http.Request, body SpawnBody) (contract.SpawnAgentsResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	return ops.SpawnAgents(r.Context(), contract.SpawnAgentsInput{ProjectSelector: selector, TaskID: taskID, Command: body.Command, Harness: body.Harness, Model: body.Model, Launcher: body.Launcher})
}

func (s *Server) showAgent(r *http.Request) (contract.ShowAgentResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ShowAgentResponse{}, err
	}
	return ops.ShowAgent(r.Context(), contract.ShowAgentInput{ProjectSelector: selector, SessionID: r.PathValue("session")})
}

func (s *Server) showTeam(r *http.Request) (contract.ShowTeamResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	return ops.ShowTeam(r.Context(), contract.ShowTeamInput{ProjectSelector: selector, TeamID: r.PathValue("team")})
}

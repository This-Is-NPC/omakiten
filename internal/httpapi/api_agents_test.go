package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"spawnAgents", http.MethodPost, "/api/v1/projects/alpha/tasks/5/agents", `{"command":"okt-task-review","harness":"opencode","model":"m","launcher":"tmux"}`, "SpawnAgents", contract.SpawnAgentsInput{
		ProjectSelector: alpha, TaskID: 5, Command: "okt-task-review", Harness: "opencode", Model: "m", Launcher: "tmux",
	}},
	{"respawnTeamMember", http.MethodPost, "/api/v1/projects/alpha/teams/t-1/members/security", `{"model":"m"}`, "SpawnAgents", contract.SpawnAgentsInput{ProjectSelector: alpha, TeamID: "t-1", Member: "security", Model: "m"}},
	{"listAgents", http.MethodGet, "/api/v1/projects/alpha/agents?task=5&status=active", "", "ListAgents", contract.ListAgentsInput{ProjectSelector: alpha, TaskID: 5, Status: "active"}},
	{"getAgent", http.MethodGet, "/api/v1/projects/alpha/agents/s-1", "", "ShowAgent", contract.ShowAgentInput{ProjectSelector: alpha, SessionID: "s-1"}},
	{"waitTaskComment", http.MethodGet, "/api/v1/projects/alpha/tasks/5/wait?tag=review-*&timeout=2m&after=9", "", "Wait", contract.WaitInput{ProjectSelector: alpha, TaskID: 5, Tag: "review-*", Timeout: "2m", AfterID: 9}},
	{"closeTeam", http.MethodPost, "/api/v1/projects/alpha/teams/t-1/close", "", "CloseTeam", contract.CloseTeamInput{ProjectSelector: alpha, TeamID: "t-1"}},
	{"getTeam", http.MethodGet, "/api/v1/projects/alpha/teams/t-1", "", "ShowTeam", contract.ShowTeamInput{ProjectSelector: alpha, TeamID: "t-1"}},
})

func (f *fakeOps) SpawnAgents(_ context.Context, in contract.SpawnAgentsInput) (contract.SpawnAgentsResponse, error) {
	f.record("SpawnAgents", in)
	return contract.SpawnAgentsResponse{}, nil
}

func (f *fakeOps) ListAgents(_ context.Context, in contract.ListAgentsInput) (contract.ListAgentsResponse, error) {
	f.record("ListAgents", in)
	return contract.ListAgentsResponse{}, nil
}

func (f *fakeOps) ShowAgent(_ context.Context, in contract.ShowAgentInput) (contract.ShowAgentResponse, error) {
	f.record("ShowAgent", in)
	return contract.ShowAgentResponse{}, nil
}

func (f *fakeOps) Wait(_ context.Context, in contract.WaitInput) (contract.WaitResponse, error) {
	f.record("Wait", in)
	return contract.WaitResponse{}, nil
}

func (f *fakeOps) CloseTeam(_ context.Context, in contract.CloseTeamInput) (contract.CloseTeamResponse, error) {
	f.record("CloseTeam", in)
	return contract.CloseTeamResponse{}, nil
}

func (f *fakeOps) ShowTeam(_ context.Context, in contract.ShowTeamInput) (contract.ShowTeamResponse, error) {
	f.record("ShowTeam", in)
	return contract.ShowTeamResponse{}, nil
}

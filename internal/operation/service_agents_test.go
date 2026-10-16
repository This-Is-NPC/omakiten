package operation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agentlaunch"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/snapstore"
)

type agentFixtureSet struct {
	ctx     context.Context
	store   *snapstore.Store
	service *Service
	project domain.Project
	task    domain.Task
	opens   *int
	fail    *bool
	sends   *[][]string
}

func agentArgs(values ...string) []domain.AgentArg {
	out := make([]domain.AgentArg, len(values))
	for i, v := range values {
		out[i] = domain.AgentArg{Values: []string{v}}
	}
	return out
}

// newAgentSpawnFixture seeds a backlog task whose bucket declares a manual
// team of a lead and one member, with a fake launcher that counts opens and
// can fail them.
func newAgentSpawnFixture(t *testing.T) agentFixtureSet {
	t.Helper()
	return newAgentTeamFixture(t, "backlog", domain.AgentTriggerManual)
}

// newAgentTeamFixture is newAgentSpawnFixture with the team declared on
// bucket with trigger.
func newAgentTeamFixture(t *testing.T, teamBucket, trigger string) agentFixtureSet {
	t.Helper()
	t.Setenv("OMAKITEN_HOME", t.TempDir())
	ctx := context.Background()
	bundle := agentTestBundle(t)
	bundle.AgentAdapters = domain.AgentAdapters{
		Defaults:  domain.AgentDefaults{Harness: "fake", Launcher: "fake"},
		Harnesses: map[string]domain.HarnessSpec{"fake": {Argv: agentArgs("fake-agent", "{prompt}")}},
		Launchers: map[string]domain.LauncherSpec{"fake": {Open: agentArgs("fake-term", "{command}"), Send: agentArgs("fake-term", "{pane}", "{text}")}},
	}
	for i := range bundle.Workflows[0].Buckets {
		if bundle.Workflows[0].Buckets[i].Key == teamBucket {
			bundle.Workflows[0].Buckets[i].Agents = &config.BucketAgentsConfig{
				Trigger: trigger,
				Lead:    config.AgentRoleConfig{Command: "okt-start"},
				Members: []config.AgentRoleConfig{{Key: "security", Command: "okt-start", Brief: "security only"}},
			}
		}
	}
	bundle.Workflows[0].Transitions = append(bundle.Workflows[0].Transitions, config.Transition{From: 2, To: 1})
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	root := t.TempDir()
	project, err := store.UpsertProject(ctx, "Agents", "agents", root)
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Review me", "Needs a team", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	svc := NewService(store, contract.ProjectSelector{CWD: root})
	svc.SetSnapshot(store.Snapshot())
	svc.SetSettings(ServiceSettings{RecentCommentLimit: 5, IncludeWorkflow: true, NextWorkLimit: 5, SimilarTaskLimit: 5})
	opens, fail := 0, false
	var sends [][]string
	svc.SetAgentLauncher(agentlaunch.Launcher{
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Run: func(_ context.Context, argv []string) ([]byte, error) {
			if fail {
				return nil, errors.New("no terminal")
			}
			if len(argv) == 3 {
				sends = append(sends, argv)
				return nil, nil
			}
			opens++
			return []byte("%" + string(rune('0'+opens))), nil
		},
	})
	return agentFixtureSet{ctx: ctx, store: store, service: svc, project: project, task: task, opens: &opens, fail: &fail, sends: &sends}
}

// TestSpawnAgentsOpensTheBucketTeamOnce verifies a spawn opens the bucket's
// lead and members with prompts and events, and a repeat returns the team.
func TestSpawnAgentsOpensTheBucketTeamOnce(t *testing.T) {
	f := newAgentSpawnFixture(t)
	resp, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	team := resp.Team
	if team.Status != domain.TeamSpawned || team.Lead.Status != domain.SessionSpawned || len(team.Members) != 1 || team.Members[0].Status != domain.SessionSpawned {
		t.Fatalf("team = %+v, want lead and member spawned", team)
	}
	assertPromptContains(t, team.Members[0].SessionID, "security only", "-T review-security", "okt team show "+team.TeamID)

	again, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil || again.Team.Status != domain.TeamRunning || again.Team.TeamID != team.TeamID || *f.opens != 2 {
		t.Fatalf("second spawn = %+v, %v (opens %d), want the running team and no new panes", again.Team, err, *f.opens)
	}

	shown, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: team.TeamID})
	if err != nil || len(shown.Team.Sessions) != 2 || shown.Team.TaskID != f.task.ID {
		t.Fatalf("ShowTeam() = %+v, %v", shown, err)
	}
	agent, err := f.service.ShowAgent(f.ctx, contract.ShowAgentInput{SessionID: team.Lead.SessionID})
	if err != nil || agent.Session.Member != domain.AgentLeadKey || agent.Bucket != "backlog" {
		t.Fatalf("ShowAgent() = %+v, %v", agent, err)
	}
	if started := countEvents(t, f, domain.EventTypeAgentSessionStarted); started != 2 {
		t.Fatalf("recorded %d agent.session_started events, want 2", started)
	}
}

// assertPromptContains reads a session's prompt file and checks each want.
func assertPromptContains(t *testing.T, sessionID string, wants ...string) {
	t.Helper()
	prompt, err := os.ReadFile(filepath.Join(os.Getenv("OMAKITEN_HOME"), "state", "agents", sessionID+".md"))
	if err != nil {
		t.Fatalf("prompt of %s missing: %v", sessionID, err)
	}
	for _, want := range wants {
		if !strings.Contains(string(prompt), want) {
			t.Fatalf("prompt of %s lacks %q:\n%s", sessionID, want, prompt)
		}
	}
}

func countEvents(t *testing.T, f agentFixtureSet, eventType string) int {
	t.Helper()
	events, err := f.store.ListEvents(f.ctx, domain.EventFilter{ProjectID: f.project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	n := 0
	for _, event := range events {
		if event.EventType == eventType {
			n++
		}
	}
	return n
}

// TestSpawnAgentsFailedLeadFreesTheTask verifies a lead that cannot open
// fails the spawn and leaves the task free for the next attempt.
func TestSpawnAgentsFailedLeadFreesTheTask(t *testing.T) {
	f := newAgentSpawnFixture(t)
	*f.fail = true
	_, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID, Command: "okt-start"})
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrAgentSpawnFailed {
		t.Fatalf("SpawnAgents() error = %v, want agent_spawn_failed", err)
	}
	*f.fail = false
	resp, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID, Command: "okt-start"})
	if err != nil || resp.Team.Status != domain.TeamSpawned || len(resp.Team.Members) != 0 {
		t.Fatalf("retry = %+v, %v, want a team of one spawned", resp.Team, err)
	}
}

// TestSpawnAgentsBlockedStoreOpensNothing verifies an uninheritable store
// stops a spawn before any pane opens.
func TestSpawnAgentsBlockedStoreOpensNothing(t *testing.T) {
	f := newAgentSpawnFixture(t)
	f.service.BlockAgentSpawn("use OMAKITEN_HOME")
	if _, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID}); err == nil || *f.opens != 0 {
		t.Fatalf("blocked spawn error = %v, opens %d; want a failure and no panes", err, *f.opens)
	}
}

// TestMoveIntoManualTeamBucketSuggestsTheTeam verifies a move into a bucket
// with a manual team opens nothing and reports the team with its spawn
// command, and that a dry run reports what the move would do.
func TestMoveIntoManualTeamBucketSuggestsTheTeam(t *testing.T) {
	f := newAgentTeamFixture(t, "dev", domain.AgentTriggerManual)
	preview, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev", DryRun: true})
	if err != nil || preview.Team == nil || preview.Team.Status != domain.TeamSuggested {
		t.Fatalf("dry run = %+v, %v; want a suggested team", preview.Team, err)
	}
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"})
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	team := moved.Team
	if team == nil || team.Status != domain.TeamSuggested || team.Spawn != fmt.Sprintf("okt agent spawn --task %d", f.task.ID) || team.Bucket != "dev" {
		t.Fatalf("team = %+v, want a suggested dev team with its spawn command", team)
	}
	if team.Lead.Harness != "fake" || len(team.Members) != 1 || team.Members[0].Key != "security" || *f.opens != 0 {
		t.Fatalf("team = %+v (opens %d), want the resolved seats and no panes", team, *f.opens)
	}
}

// TestDryRunIntoAutoTeamBucketWouldSpawn verifies a dry run into a bucket
// with an automatic team reports would_spawn without opening panes, and a
// move between buckets without teams reports none.
func TestDryRunIntoAutoTeamBucketWouldSpawn(t *testing.T) {
	f := newAgentTeamFixture(t, "dev", domain.AgentTriggerAuto)
	preview, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev", DryRun: true})
	if err != nil || preview.Team == nil || preview.Team.Status != domain.TeamWouldSpawn || preview.Team.Lead.SessionID != "" || *f.opens != 0 {
		t.Fatalf("dry run = %+v, %v (opens %d); want would_spawn and no panes", preview.Team, err, *f.opens)
	}
	f = newAgentTeamFixture(t, "done", domain.AgentTriggerAuto)
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"})
	if err != nil || moved.Team != nil {
		t.Fatalf("move without a team = %+v, %v; want no team", moved.Team, err)
	}
}

// TestMoveIntoAutoTeamBucketSpawnsUnlessSkipped verifies a move into a
// bucket with an automatic team opens it and reports it, and --no-spawn
// reports it skipped without opening panes.
func TestMoveIntoAutoTeamBucketSpawnsUnlessSkipped(t *testing.T) {
	f := newAgentTeamFixture(t, "dev", domain.AgentTriggerAuto)
	skipped, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev", NoSpawn: true})
	if err != nil || skipped.Team == nil || skipped.Team.Status != domain.TeamSkipped || *f.opens != 0 {
		t.Fatalf("--no-spawn move = %+v, %v (opens %d); want skipped and no panes", skipped.Team, err, *f.opens)
	}

	f = newAgentTeamFixture(t, "dev", domain.AgentTriggerAuto)
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"})
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.Task.BucketKey != "dev" || moved.Team == nil || moved.Team.Status != domain.TeamSpawned || moved.Team.Lead.Pane == "" || *f.opens != 2 {
		t.Fatalf("move = %+v (opens %d), want the task in dev and its team spawned", moved, *f.opens)
	}
}

// TestMemberResultWakesTheLead verifies a review-<key> comment marks that
// member done, records team.member_done, and types one line into the lead's
// pane.
func TestMemberResultWakesTheLead(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{TaskID: f.task.ID, Body: "no findings", AuthorType: "agent", Tags: []string{"review-security"}}); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	team, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: spawned.Team.TeamID})
	if err != nil {
		t.Fatalf("ShowTeam() error = %v", err)
	}
	if team.Team.Sessions[1].Status != domain.SessionDone || len(team.Results) != 1 || team.Results[0].Member != "security" {
		t.Fatalf("team = %+v, want security done with its result", team)
	}
	if len(*f.sends) != 1 || (*f.sends)[0][1] != spawned.Team.Lead.Pane || !strings.Contains((*f.sends)[0][2], "security finished task") {
		t.Fatalf("sends = %q, want one wake line to the lead's pane %s", *f.sends, spawned.Team.Lead.Pane)
	}
	if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{TaskID: f.task.ID, Body: "again", AuthorType: "agent", Tags: []string{"review-security"}}); err != nil {
		t.Fatalf("AddComment(again) error = %v", err)
	}
	if len(*f.sends) != 1 {
		t.Fatalf("a second result woke the lead again: %q", *f.sends)
	}
}

// TestAddressedMessageWakesTheRecipient verifies a to-<key> comment types
// one line into that seat's pane, and an unknown key does nothing.
func TestAddressedMessageWakesTheRecipient(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	for _, tag := range []string{"to-security", "to-nobody"} {
		if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{TaskID: f.task.ID, Body: "question", AuthorType: "agent", Tags: []string{tag}}); err != nil {
			t.Fatalf("AddComment(%s) error = %v", tag, err)
		}
	}
	want := fmt.Sprintf("okt: message for security on task #%d — okt comment list %d --tag to-security", f.task.ID, f.task.ID)
	if len(*f.sends) != 1 || (*f.sends)[0][1] != spawned.Team.Members[0].Pane || (*f.sends)[0][2] != want {
		t.Fatalf("sends = %q, want %q to the security pane %s", *f.sends, want, spawned.Team.Members[0].Pane)
	}
}

// TestTeamClosesWhenTheTaskLeavesItsBucket verifies a move out of the
// team's bucket closes the team, stops its running seats with one line each,
// and a later spawn in that bucket opens a new team.
func TestTeamClosesWhenTheTaskLeavesItsBucket(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	if _, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"}); err != nil {
		t.Fatalf("MoveTask(dev) error = %v", err)
	}
	team, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: spawned.Team.TeamID})
	if err != nil || team.Team.Status != domain.TeamClosed {
		t.Fatalf("team after leaving = %+v, %v; want closed", team.Team, err)
	}
	for _, session := range team.Team.Sessions {
		if session.Status != domain.SessionStopped {
			t.Fatalf("session %s status = %s, want stopped", session.Member, session.Status)
		}
	}
	stop := fmt.Sprintf("okt: task #%d left backlog; stop working on it", f.task.ID)
	if len(*f.sends) != 2 || (*f.sends)[0][2] != stop {
		t.Fatalf("sends = %q, want %q to both panes", *f.sends, stop)
	}
	if _, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "backlog"}); err != nil {
		t.Fatalf("MoveTask(backlog) error = %v", err)
	}
	again, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil || again.Team.Status != domain.TeamSpawned || again.Team.TeamID == spawned.Team.TeamID {
		t.Fatalf("spawn after closing = %+v, %v; want a new team", again.Team, err)
	}
}

// TestCloseTeamStopsItsSeats verifies team close has the same effect as
// leaving the bucket and refuses a team that is not active.
func TestCloseTeamStopsItsSeats(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	closed, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: spawned.Team.TeamID})
	if err != nil || closed.Team.Status != domain.TeamClosed || len(*f.sends) != 2 {
		t.Fatalf("CloseTeam() = %+v, %v (sends %q); want closed and both seats told", closed.Team, err, *f.sends)
	}
	if _, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: spawned.Team.TeamID}); err == nil {
		t.Fatal("closing a closed team succeeded")
	}
}

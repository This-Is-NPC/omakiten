package operation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"omakiten/internal/agentlaunch"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/snapstore"
)

type agentFixtureSet struct {
	ctx     context.Context
	store   *snapstore.Store
	dbPath  string
	service *Service
	project domain.Project
	task    domain.Task
	agents  *fakeAgents
}

// fakeAgents is the real launcher with a fake detach that records each
// supervisor start, and fake liveness and stops; realStop makes Stop end
// the real process group too.
type fakeAgents struct {
	agentlaunch.Launcher
	mu       sync.Mutex
	starts   [][]string
	fail     bool
	alive    map[string]bool
	stopped  []int
	realStop bool
}

func (f *fakeAgents) Alive(sessionID string) bool { return f.alive[sessionID] }

func (f *fakeAgents) Stop(pid int) error {
	f.stopped = append(f.stopped, pid)
	if f.realStop {
		return f.Launcher.Stop(pid)
	}
	return nil
}

func agentArgs(values ...string) []domain.AgentArg {
	out := make([]domain.AgentArg, len(values))
	for i, v := range values {
		out[i] = domain.AgentArg{Values: []string{v}}
	}
	return out
}

// newAgentSpawnFixture seeds a backlog task whose bucket declares a manual
// team of a lead and one member, with a fake launcher that records starts
// and can fail them.
func newAgentSpawnFixture(t *testing.T) agentFixtureSet {
	t.Helper()
	return newAgentTeamFixture(t, "backlog", domain.AgentTriggerManual)
}

// newAgentTeamFixture is newAgentSpawnFixture with the team declared on
// bucket with trigger, starting the seats start names when it opens.
func newAgentTeamFixture(t *testing.T, teamBucket, trigger string, start ...string) agentFixtureSet {
	t.Helper()
	t.Setenv("OMAKITEN_HOME", t.TempDir())
	ctx := context.Background()
	bundle := agentTestBundle(t)
	bundle.Personas = append(bundle.Personas, config.Persona{Slug: "auditor", Name: "Auditor", Body: "Trust nothing you did not verify.", SkillRepertoire: []string{"okt-start"}})
	bundle.AgentAdapters = domain.AgentAdapters{
		Defaults:  domain.AgentDefaults{Harness: "fake"},
		Harnesses: map[string]domain.HarnessSpec{"fake": {Argv: agentArgs("fake-agent", "--", "{prompt}")}},
	}
	for i := range bundle.Workflows[0].Buckets {
		if bundle.Workflows[0].Buckets[i].Key == teamBucket {
			bundle.Workflows[0].Buckets[i].Agents = &config.BucketAgentsConfig{
				Trigger: trigger,
				Lead:    config.AgentRoleConfig{Command: "okt-start"},
				Members: []config.AgentRoleConfig{{Key: "security", Command: "okt-start", Persona: "auditor", Brief: "security only"}},
				Start:   start,
			}
		}
	}
	bundle.Workflows[0].Transitions = append(bundle.Workflows[0].Transitions, config.Transition{From: 2, To: 1})
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	store := snapstore.Open(t, dbPath)
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
	agents := &fakeAgents{alive: map[string]bool{}}
	agents.Launcher = agentlaunch.Launcher{
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Detach: func(argv, _ []string, _, _ string) error {
			agents.mu.Lock()
			defer agents.mu.Unlock()
			if agents.fail {
				return errors.New("cannot start")
			}
			agents.starts = append(agents.starts, argv)
			return nil
		},
	}
	svc.SetAgentLauncher(agents)
	return agentFixtureSet{ctx: ctx, store: store, dbPath: dbPath, service: svc, project: project, task: task, agents: agents}
}

// harnessStarted records pid as the harness of each session, as its
// supervisor does once the harness runs, and marks the sessions alive.
func (f agentFixtureSet) harnessStarted(t *testing.T, pids map[string]int) {
	t.Helper()
	for id, pid := range pids {
		if err := f.store.SetAgentSessionPID(f.ctx, id, pid); err != nil {
			t.Fatal(err)
		}
		f.agents.alive[id] = true
	}
}

// run supervises sessionID with argv as the harness, as `okt agent run`
// does.
func (f agentFixtureSet) run(t *testing.T, sessionID string, timeout time.Duration, argv ...string) domain.AgentSession {
	t.Helper()
	resp, err := f.service.RunAgentSession(f.ctx, contract.RunAgentSessionInput{SessionID: sessionID, Argv: argv, Timeout: timeout})
	if err != nil {
		t.Fatalf("RunAgentSession(%s) error = %v", sessionID, err)
	}
	return resp.Session
}

// TestSpawnAgentsStartsTheBucketTeamOnce verifies a spawn starts a
// supervisor for the bucket's lead and members with prompts and events, and
// a repeat returns the team.
func TestSpawnAgentsStartsTheBucketTeamOnce(t *testing.T) {
	f := newAgentSpawnFixture(t)
	resp, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	team := resp.Team
	if team.Status != domain.TeamStarted || team.Lead.Status != domain.TeamStarted || len(team.Members) != 1 || team.Members[0].Status != domain.TeamStarted {
		t.Fatalf("team = %+v, want lead and member started", team)
	}
	if !strings.HasSuffix(team.Lead.Transcript, team.Lead.SessionID+".jsonl") {
		t.Fatalf("lead transcript = %q, want the session's JSONL file", team.Lead.Transcript)
	}
	assertPromptContains(t, team.Members[0].SessionID, "security only", "-T review-security", "okt team show "+team.TeamID, "You run once")
	var supervisors []string
	for _, argv := range f.agents.starts {
		supervisors = append(supervisors, strings.Join(argv, " "))
	}
	if !slices.ContainsFunc(supervisors, func(argv string) bool {
		return strings.Contains(argv, " --project agents agent run --session "+team.Lead.SessionID+" -- fake-agent -- ")
	}) {
		t.Fatalf("supervisor argv = %q, want agent run for the lead's session", supervisors)
	}

	again, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil || again.Team.Status != domain.TeamRunning || again.Team.TeamID != team.TeamID || len(f.agents.starts) != 2 {
		t.Fatalf("second spawn = %+v, %v (starts %d), want the running team and no new supervisor", again.Team, err, len(f.agents.starts))
	}

	shown, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: team.TeamID})
	if err != nil || len(shown.Team.Sessions) != 2 || shown.Team.TaskID != f.task.ID {
		t.Fatalf("ShowTeam() = %+v, %v", shown, err)
	}
	agent, err := f.service.ShowAgent(f.ctx, contract.ShowAgentInput{SessionID: team.Lead.SessionID})
	if err != nil || agent.Session.Member != domain.AgentLeadKey || agent.Bucket != "backlog" || agent.Session.Status != domain.SessionRunning {
		t.Fatalf("ShowAgent() = %+v, %v", agent, err)
	}
	if started := countEvents(t, f, domain.EventTypeAgentSessionStarted); started != 2 {
		t.Fatalf("recorded %d agent.session_started events, want 2", started)
	}
}

// TestSeatPersonaReplacesTheCommandPersona verifies a seat's persona
// replaces the one its command binds in the prompt and the outcome, and a
// seat without one keeps the binding's.
func TestSeatPersonaReplacesTheCommandPersona(t *testing.T) {
	f := newAgentSpawnFixture(t)
	resp, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	member, lead := resp.Team.Members[0], resp.Team.Lead
	if member.Persona != "auditor" || lead.Persona != "agent" {
		t.Fatalf("personas = member %q, lead %q; want auditor and the binding's agent", member.Persona, lead.Persona)
	}
	assertPromptContains(t, member.SessionID, "## Persona — Auditor", "Trust nothing you did not verify.", "Start work.")
	assertPromptContains(t, lead.SessionID, "## Persona — Agent")
	shown, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: resp.Team.TeamID})
	if err != nil || teamSeat(shown.Team, "security").Persona != "auditor" {
		t.Fatalf("ShowTeam() = %+v, %v; want the member's session on persona auditor", shown.Team, err)
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

// TestSpawnAgentsFailedLeadFreesTheTask verifies a lead that cannot start
// fails the spawn, finishes its session failed, and leaves the task free
// for the next attempt.
func TestSpawnAgentsFailedLeadFreesTheTask(t *testing.T) {
	f := newAgentSpawnFixture(t)
	f.agents.fail = true
	_, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID, Command: "okt-start"})
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrAgentSpawnFailed {
		t.Fatalf("SpawnAgents() error = %v, want agent_spawn_failed", err)
	}
	if finished := countEvents(t, f, domain.EventTypeAgentSessionFinished); finished != 1 || countEvents(t, f, domain.EventTypeTeamFinished) != 0 {
		t.Fatalf("recorded %d agent.session_finished events and a team.finished, want only the failed lead's finish", finished)
	}
	f.agents.fail = false
	resp, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID, Command: "okt-start"})
	if err != nil || resp.Team.Status != domain.TeamStarted || len(resp.Team.Members) != 0 {
		t.Fatalf("retry = %+v, %v, want a team of one started", resp.Team, err)
	}
}

// TestRunAgentSessionRecordsHowTheHarnessEnded verifies the supervisor
// writes the harness output to the transcript and finishes the session
// from its exit code, its timeout, or its stop, each once.
func TestRunAgentSessionRecordsHowTheHarnessEnded(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	lead := f.run(t, spawned.Team.Lead.SessionID, 0, "sh", "-c", `echo '{"type":"result"}'`)
	if lead.Status != domain.SessionDone || lead.ExitCode == nil || *lead.ExitCode != 0 || lead.FinishedAt == "" || lead.PID == 0 {
		t.Fatalf("lead = %+v, want done with exit 0 and the harness pid", lead)
	}
	if transcript, err := os.ReadFile(lead.Transcript); err != nil || !strings.Contains(string(transcript), `{"type":"result"}`) {
		t.Fatalf("transcript = %q, %v; want the harness output", transcript, err)
	}
	member := f.run(t, spawned.Team.Members[0].SessionID, 0, "sh", "-c", "exit 2")
	if member.Status != domain.SessionFailed || *member.ExitCode != 2 || member.Error != "exit status 2" {
		t.Fatalf("member = %+v, want failed with exit 2", member)
	}
	if again := f.run(t, member.ID, 0, "sh", "-c", "exit 0"); again.Status != domain.SessionFailed {
		t.Fatalf("a second run rewrote a finished session: %+v", again)
	}
	if finished := countEvents(t, f, domain.EventTypeAgentSessionFinished); finished != 2 {
		t.Fatalf("recorded %d agent.session_finished events, want one per session", finished)
	}

	f = newAgentSpawnFixture(t)
	spawned, err = f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID, Command: "okt-start"})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	timedOut := f.run(t, spawned.Team.Lead.SessionID, 100*time.Millisecond, "sleep", "30")
	if timedOut.Status != domain.SessionFailed || timedOut.Error != "timed out after 100ms" {
		t.Fatalf("timed out run = %+v, want failed by its timeout", timedOut)
	}
}

// TestASeatStoppedBeforeItsHarnessStartsStopsAtOnce verifies a supervisor
// whose session was stopped before the harness ran stops the harness as
// soon as it starts, and leaves the session stopped.
func TestASeatStoppedBeforeItsHarnessStartsStopsAtOnce(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID, Command: "okt-start"})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	if _, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: spawned.Team.TeamID}); err != nil {
		t.Fatalf("CloseTeam() error = %v", err)
	}
	f.agents.realStop = true
	start := time.Now()
	session := f.run(t, spawned.Team.Lead.SessionID, 0, "sleep", "30")
	if session.Status != domain.SessionStopped || len(f.agents.stopped) != 1 || time.Since(start) > 5*time.Second {
		t.Fatalf("run = %+v after %s (stopped %v), want the harness stopped at once", session, time.Since(start), f.agents.stopped)
	}
}

// TestTeamFinishedFollowsTheLastRun verifies team.finished is recorded
// once the last running seat of an active team ends, with the team's
// counts, and not before.
func TestTeamFinishedFollowsTheLastRun(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	f.run(t, spawned.Team.Lead.SessionID, 0, "true")
	if n := countEvents(t, f, domain.EventTypeTeamFinished); n != 0 {
		t.Fatalf("team.finished recorded %d times while the member runs", n)
	}
	f.run(t, spawned.Team.Members[0].SessionID, 0, "false")
	events, err := f.store.ListEvents(f.ctx, domain.EventFilter{ProjectID: f.project.ID})
	if err != nil {
		t.Fatal(err)
	}
	var payloads []string
	for _, event := range events {
		if event.EventType == domain.EventTypeTeamFinished {
			payloads = append(payloads, event.Payload)
		}
	}
	want := fmt.Sprintf(`"bucket":"backlog","done":1,"failed":1,"task_id":%d,"team_id":"%s"`, f.task.ID, spawned.Team.TeamID)
	if len(payloads) != 1 || !strings.Contains(payloads[0], want) {
		t.Fatalf("team.finished payloads = %q, want one with %s", payloads, want)
	}
}

// TestTeamStartsOnlyItsStartSeats verifies a team whose start lists its
// members opens with the lead waiting, and SpawnSeat starts the lead once,
// by team or by task, leaving a seat that already ran as it is.
func TestTeamStartsOnlyItsStartSeats(t *testing.T) {
	f := newAgentTeamFixture(t, "backlog", domain.AgentTriggerManual, "security")
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	if team := spawned.Team; team.Lead.Status != domain.TeamWaiting || team.Lead.SessionID != "" || team.Members[0].Status != domain.TeamStarted || len(f.agents.starts) != 1 {
		t.Fatalf("team = %+v (starts %d), want the lead waiting and the member started", team, len(f.agents.starts))
	}
	lead, err := f.service.SpawnSeat(f.ctx, contract.SpawnSeatInput{TeamID: spawned.Team.TeamID, Member: domain.AgentLeadKey})
	if err != nil || lead.Team.Status != domain.TeamStarted || lead.Team.Lead.SessionID == "" || len(f.agents.starts) != 2 {
		t.Fatalf("SpawnSeat(lead) = %+v, %v; want the lead started", lead.Team, err)
	}
	again, err := f.service.SpawnSeat(f.ctx, contract.SpawnSeatInput{TaskID: f.task.ID, Member: domain.AgentLeadKey})
	if err != nil || again.Team.Status != domain.TeamRunning || again.Team.Lead.SessionID != lead.Team.Lead.SessionID || len(f.agents.starts) != 2 {
		t.Fatalf("second SpawnSeat(lead) = %+v, %v (starts %d); want the seat left as it is", again.Team, err, len(f.agents.starts))
	}
	if _, err := f.service.SpawnSeat(f.ctx, contract.SpawnSeatInput{TeamID: spawned.Team.TeamID, Member: "perf"}); err == nil {
		t.Fatal("SpawnSeat started a key outside the roster")
	}
	if _, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: spawned.Team.TeamID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SpawnSeat(f.ctx, contract.SpawnSeatInput{TaskID: f.task.ID, Member: domain.AgentLeadKey}); err == nil {
		t.Fatal("SpawnSeat started a seat for a task without an active team")
	}
}

// TestOrphanedSessionsFinishFailed verifies a running session whose
// supervisor no longer holds its lock finishes failed on the next read,
// while a live one keeps running.
func TestOrphanedSessionsFinishFailed(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	db, err := sql.Open("sqlite", f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE agent_sessions SET created_at = datetime('now', '-2 hours')`); err != nil {
		t.Fatal(err)
	}
	f.agents.alive[spawned.Team.Lead.SessionID] = true
	shown, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: spawned.Team.TeamID})
	if err != nil {
		t.Fatalf("ShowTeam() error = %v", err)
	}
	lead, member := teamSeat(shown.Team, domain.AgentLeadKey), teamSeat(shown.Team, "security")
	if lead.Status != domain.SessionRunning || member.Status != domain.SessionFailed || !strings.HasPrefix(member.Error, "orphaned") {
		t.Fatalf("sessions = %+v / %+v, want the live lead running and the orphan failed", lead, member)
	}
}

// TestShowTeamNamesMembersDoneWithoutAResult verifies a member that exited
// 0 without publishing review-<key> is reported until it publishes one.
func TestShowTeamNamesMembersDoneWithoutAResult(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	f.run(t, spawned.Team.Members[0].SessionID, 0, "true")
	shown, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: spawned.Team.TeamID})
	if err != nil || len(shown.MissingResults) != 1 || shown.MissingResults[0] != "security" {
		t.Fatalf("ShowTeam() = %+v, %v; want security missing its result", shown, err)
	}
	if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{TaskID: f.task.ID, Body: "no findings", AuthorType: "agent", Tags: []string{"review-security"}}); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	if shown, err = f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: spawned.Team.TeamID}); err != nil || len(shown.MissingResults) != 0 || len(shown.Results) != 1 {
		t.Fatalf("ShowTeam() after the result = %+v, %v", shown, err)
	}
}

// TestSpawnAgentsBlockedStoreStartsNothing verifies an uninheritable store
// stops a spawn before any supervisor starts.
func TestSpawnAgentsBlockedStoreStartsNothing(t *testing.T) {
	f := newAgentSpawnFixture(t)
	f.service.BlockAgentSpawn("use OMAKITEN_HOME")
	if _, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID}); err == nil || len(f.agents.starts) != 0 {
		t.Fatalf("blocked spawn error = %v, starts %d; want a failure and nothing started", err, len(f.agents.starts))
	}
}

// TestMoveIntoManualTeamBucketSuggestsTheTeam verifies a move into a bucket
// with a manual team starts nothing and reports the team with its spawn
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
	if team.Lead.Harness != "fake" || len(team.Members) != 1 || team.Members[0].Key != "security" || len(f.agents.starts) != 0 {
		t.Fatalf("team = %+v (starts %d), want the resolved seats and nothing started", team, len(f.agents.starts))
	}
}

// TestDryRunIntoAutoTeamBucketWouldSpawn verifies a dry run into a bucket
// with an automatic team reports would_spawn without starting anything, and a
// move between buckets without teams reports none.
func TestDryRunIntoAutoTeamBucketWouldSpawn(t *testing.T) {
	f := newAgentTeamFixture(t, "dev", domain.AgentTriggerAuto)
	preview, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev", DryRun: true})
	if err != nil || preview.Team == nil || preview.Team.Status != domain.TeamWouldSpawn || preview.Team.Lead.SessionID != "" || len(f.agents.starts) != 0 {
		t.Fatalf("dry run = %+v, %v (starts %d); want would_spawn and nothing started", preview.Team, err, len(f.agents.starts))
	}
	f = newAgentTeamFixture(t, "done", domain.AgentTriggerAuto)
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"})
	if err != nil || moved.Team != nil {
		t.Fatalf("move without a team = %+v, %v; want no team", moved.Team, err)
	}
}

// TestMoveIntoAutoTeamBucketSpawnsUnlessSkipped verifies a move into a
// bucket with an automatic team starts it and reports it, and --no-spawn
// reports it skipped without starting anything.
func TestMoveIntoAutoTeamBucketSpawnsUnlessSkipped(t *testing.T) {
	f := newAgentTeamFixture(t, "dev", domain.AgentTriggerAuto)
	skipped, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev", NoSpawn: true})
	if err != nil || skipped.Team == nil || skipped.Team.Status != domain.TeamSkipped || len(f.agents.starts) != 0 {
		t.Fatalf("--no-spawn move = %+v, %v (starts %d); want skipped and nothing started", skipped.Team, err, len(f.agents.starts))
	}

	f = newAgentTeamFixture(t, "dev", domain.AgentTriggerAuto)
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"})
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.Task.BucketKey != "dev" || moved.Team == nil || moved.Team.Status != domain.TeamStarted || moved.Team.Lead.Transcript == "" || len(f.agents.starts) != 2 {
		t.Fatalf("move = %+v (starts %d), want the task in dev and its team started", moved, len(f.agents.starts))
	}
}

// TestTeamClosesWhenTheTaskLeavesItsBucket verifies a move out of the
// team's bucket closes the team, stops its running seats and terminates
// the supervisors still alive, and a later spawn in that bucket opens a new
// team.
func TestTeamClosesWhenTheTaskLeavesItsBucket(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	f.harnessStarted(t, map[string]int{spawned.Team.Lead.SessionID: 1001})
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
	if !slices.Equal(f.agents.stopped, []int{1001}) {
		t.Fatalf("stopped = %v, want only the running lead's harness", f.agents.stopped)
	}
	if _, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "backlog"}); err != nil {
		t.Fatalf("MoveTask(backlog) error = %v", err)
	}
	again, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil || again.Team.Status != domain.TeamStarted || again.Team.TeamID == spawned.Team.TeamID {
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
	f.harnessStarted(t, map[string]int{spawned.Team.Lead.SessionID: 1001, spawned.Team.Members[0].SessionID: 1002})
	closed, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: spawned.Team.TeamID})
	if err != nil || closed.Team.Status != domain.TeamClosed || len(f.agents.stopped) != 2 {
		t.Fatalf("CloseTeam() = %+v, %v (stopped %v); want closed and both harnesses stopped", closed.Team, err, f.agents.stopped)
	}
	if _, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: spawned.Team.TeamID}); err == nil {
		t.Fatal("closing a closed team succeeded")
	}
}

// TestRespawnMemberReplacesItsSession verifies --team/--member starts a new
// session for a roster key, stops and replaces the old one, and refuses
// keys outside the roster.
func TestRespawnMemberReplacesItsSession(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	old := spawned.Team.Members[0].SessionID
	f.harnessStarted(t, map[string]int{old: 1002})
	again, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TeamID: spawned.Team.TeamID, Member: "security"})
	if err != nil || again.Team.Status != domain.TeamStarted || len(again.Team.Members) != 1 || again.Team.Members[0].SessionID == old {
		t.Fatalf("respawn = %+v, %v; want a new security session", again.Team, err)
	}
	if !slices.Equal(f.agents.stopped, []int{1002}) {
		t.Fatalf("stopped = %v, want the replaced seat's harness", f.agents.stopped)
	}
	assertPromptContains(t, again.Team.Members[0].SessionID, "security only", "-T review-security")
	team, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: spawned.Team.TeamID})
	if err != nil {
		t.Fatalf("ShowTeam() error = %v", err)
	}
	statuses := map[string]string{}
	for _, session := range team.Team.Sessions {
		statuses[session.ID] = session.Status
	}
	if statuses[old] != domain.SessionReplaced || statuses[again.Team.Members[0].SessionID] != domain.SessionRunning {
		t.Fatalf("sessions = %v, want the old one replaced and the new one running", statuses)
	}
	if _, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TeamID: spawned.Team.TeamID, Member: "perf"}); err == nil {
		t.Fatal("respawning a key outside the roster succeeded")
	}
}

// TestWaitDefaultsToTheActiveTeamsStart verifies a wait without --after on
// a task with an active team sees results published since the team opened.
func TestWaitDefaultsToTheActiveTeamsStart(t *testing.T) {
	f := newAgentSpawnFixture(t)
	if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{TaskID: f.task.ID, Body: "before the team", AuthorType: "agent", Tags: []string{"review-old"}}); err != nil {
		t.Fatalf("AddComment(before) error = %v", err)
	}
	if _, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID}); err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{TaskID: f.task.ID, Body: "done", AuthorType: "agent", Tags: []string{"review-security"}}); err != nil {
		t.Fatalf("AddComment(result) error = %v", err)
	}
	got, err := f.service.Wait(f.ctx, contract.WaitInput{TaskID: f.task.ID, Tag: "review-*", Timeout: "300ms"})
	if err != nil || got.TimedOut || len(got.Matched) != 1 || got.Matched[0].Tags[0].Name != "review-security" {
		t.Fatalf("Wait() = %+v, %v; want the result published after the team opened", got, err)
	}
}

// TestListAgentsFiltersTeams verifies agent.list returns the project's teams
// newest first with their sessions, filtered by task and status.
func TestListAgentsFiltersTeams(t *testing.T) {
	f := newAgentSpawnFixture(t)
	first, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	if _, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: first.Team.TeamID}); err != nil {
		t.Fatalf("CloseTeam() error = %v", err)
	}
	second, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents(again) error = %v", err)
	}
	all, err := f.service.ListAgents(f.ctx, contract.ListAgentsInput{TaskID: f.task.ID})
	if err != nil || len(all.Teams) != 2 || all.Teams[0].ID != second.Team.TeamID || len(all.Teams[0].Sessions) != 2 {
		t.Fatalf("ListAgents(task) = %+v, %v; want both teams newest first with sessions", all.Teams, err)
	}
	active, err := f.service.ListAgents(f.ctx, contract.ListAgentsInput{Status: domain.TeamStatusActive})
	if err != nil || len(active.Teams) != 1 || active.Teams[0].ID != second.Team.TeamID {
		t.Fatalf("ListAgents(active) = %+v, %v; want only the open team", active.Teams, err)
	}
	if _, err := f.service.ListAgents(f.ctx, contract.ListAgentsInput{Status: "running"}); err == nil {
		t.Fatal("ListAgents accepted an unknown status")
	}
}

// TestTaskReadsShowTheActiveTeam verifies task show and task continue carry
// the task's active team, and nothing once it closes.
func TestTaskReadsShowTheActiveTeam(t *testing.T) {
	f := newAgentSpawnFixture(t)
	spawned, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("SpawnAgents() error = %v", err)
	}
	shown, err := f.service.ShowTask(f.ctx, contract.ShowTaskInput{TaskID: f.task.ID})
	if err != nil || shown.Team == nil || shown.Team.ID != spawned.Team.TeamID || len(shown.Team.Sessions) != 2 {
		t.Fatalf("ShowTask().Team = %+v, %v; want the active team", shown.Team, err)
	}
	continued, err := f.service.ContinueTask(f.ctx, contract.ContinueTaskInput{TaskID: f.task.ID})
	if err != nil || continued.Team == nil || continued.Team.ID != spawned.Team.TeamID {
		t.Fatalf("ContinueTask().Team = %+v, %v; want the active team", continued.Team, err)
	}
	if _, err := f.service.CloseTeam(f.ctx, contract.CloseTeamInput{TeamID: spawned.Team.TeamID}); err != nil {
		t.Fatalf("CloseTeam() error = %v", err)
	}
	if shown, err = f.service.ShowTask(f.ctx, contract.ShowTaskInput{TaskID: f.task.ID}); err != nil || shown.Team != nil {
		t.Fatalf("ShowTask().Team after close = %+v, %v; want none", shown.Team, err)
	}
}

// TestUnconfiguredTeamLeavesTheMoveWhole verifies a team whose seats name no
// harness, with nothing in preset or preferences, starts nothing, reports
// unconfigured, and does not fail the move.
func TestUnconfiguredTeamLeavesTheMoveWhole(t *testing.T) {
	f := newAgentTeamFixture(t, "dev", domain.AgentTriggerAuto)
	adapters := f.store.Snapshot().AgentAdapters()
	adapters.Defaults, adapters.Fallback = domain.AgentDefaults{}, domain.AgentDefaults{}
	bundle := agentTestBundle(t)
	bundle.AgentAdapters = adapters
	for i := range bundle.Workflows[0].Buckets {
		if bundle.Workflows[0].Buckets[i].Key == "dev" {
			bundle.Workflows[0].Buckets[i].Agents = &config.BucketAgentsConfig{Trigger: domain.AgentTriggerAuto, Lead: config.AgentRoleConfig{Command: "okt-start"}}
		}
	}
	if err := f.store.ImportBundle(f.ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatal(err)
	}
	f.service.SetSnapshot(f.store.Snapshot())
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"})
	if err != nil {
		t.Fatalf("MoveTask() error = %v, want the move to succeed", err)
	}
	team := moved.Team
	if moved.Task.BucketKey != "dev" || team == nil || team.Status != domain.TeamUnconfigured || team.Lead.Status != domain.TeamUnconfigured || len(f.agents.starts) != 0 {
		t.Fatalf("move = %+v (starts %d), want the task moved and its team unconfigured", moved, len(f.agents.starts))
	}
	if team.Error == nil || team.Error.Code != "agent_unconfigured" || !strings.Contains(team.Error.Message, "preferences.yaml") {
		t.Fatalf("team error = %+v, want agent_unconfigured pointing at preferences.yaml", team.Error)
	}
	assertPointsAtPreferences(t, f, team)
}

// assertPointsAtPreferences checks an unconfigured team's errors, and an
// explicit spawn's failure, name the global config path command and the
// absolute preferences file.
func assertPointsAtPreferences(t *testing.T, f agentFixtureSet, team *domain.TeamOutcome) {
	t.Helper()
	prefs := filepath.Join(os.Getenv("OMAKITEN_HOME"), "preferences.yaml")
	for name, perr := range map[string]*domain.AgentError{"team": team.Error, "lead": team.Lead.Error} {
		if !strings.Contains(perr.Message, "okt config path --scope global") || perr.PreferencesPath != prefs {
			t.Fatalf("%s error = %+v, want `okt config path --scope global` and preferences_path %s", name, perr, prefs)
		}
	}
	_, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: f.task.ID})
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Details["preferences_path"] != prefs {
		t.Fatalf("explicit spawn error = %v, want details.preferences_path %s", err, prefs)
	}
}

// TestManualTeamSuggestsWhatIsMissing verifies a manual team whose lead names
// no harness is still suggested, with the seat saying what is missing.
func TestManualTeamSuggestsWhatIsMissing(t *testing.T) {
	f := newAgentTeamFixture(t, "dev", domain.AgentTriggerManual)
	bundle := agentTestBundle(t)
	bundle.AgentAdapters = f.store.Snapshot().AgentAdapters()
	bundle.AgentAdapters.Defaults = domain.AgentDefaults{}
	for i := range bundle.Workflows[0].Buckets {
		if bundle.Workflows[0].Buckets[i].Key == "dev" {
			bundle.Workflows[0].Buckets[i].Agents = &config.BucketAgentsConfig{Trigger: domain.AgentTriggerManual, Lead: config.AgentRoleConfig{Command: "okt-start"}}
		}
	}
	if err := f.store.ImportBundle(f.ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatal(err)
	}
	f.service.SetSnapshot(f.store.Snapshot())
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev"})
	if err != nil || moved.Team == nil || moved.Team.Status != domain.TeamSuggested {
		t.Fatalf("move = %+v, %v; want a suggested team", moved.Team, err)
	}
	if lead := moved.Team.Lead; lead.Status != domain.TeamUnconfigured || lead.Error == nil || lead.Error.Code != "agent_unconfigured" {
		t.Fatalf("lead = %+v, want unconfigured with what is missing", lead)
	}
}

// TestMaxSessionsHoldsAcrossConcurrentSpawns verifies spawns racing for
// the last session slot start exactly one seat between them.
func TestMaxSessionsHoldsAcrossConcurrentSpawns(t *testing.T) {
	f := newAgentSpawnFixture(t)
	bundle := agentTestBundle(t)
	bundle.AgentAdapters = f.store.Snapshot().AgentAdapters()
	bundle.AgentAdapters.MaxSessions = 1
	if err := f.store.ImportBundle(f.ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatal(err)
	}
	f.service.SetSnapshot(f.store.Snapshot())
	tasks := []int64{f.task.ID}
	for i := range 5 {
		task, err := f.store.CreateTask(f.ctx, f.project.ID, fmt.Sprintf("Race %d", i), "Races for the slot", domain.Priority(2), "backlog", nil, f.store.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task.ID)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	started, refused := 0, 0
	for _, id := range tasks {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{TaskID: id, Command: "okt-start"})
			mu.Lock()
			defer mu.Unlock()
			var coded *domain.CodedError
			switch {
			case err == nil:
				started++
			case errors.As(err, &coded) && coded.Details["team"].(domain.TeamOutcome).Error.Code == "max_sessions":
				refused++
			default:
				t.Errorf("SpawnAgents(%d) error = %v (%+v)", id, err, coded.Details["team"].(domain.TeamOutcome).Error)
			}
		}(id)
	}
	wg.Wait()
	if started != 1 || refused != len(tasks)-1 {
		t.Fatalf("started %d, refused %d; want exactly one seat in the only slot", started, refused)
	}
}

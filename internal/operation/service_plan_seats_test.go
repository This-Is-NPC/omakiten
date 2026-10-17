package operation

import (
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// newPlanSeatFixture opens a workflow backlog→dev→done whose plans block
// declares the plan seat auditor on persona auditor and gates completion
// on its review-auditor result, with plan "p" holding the fixture's task
// and a second one in wave "build", both in dev.
func newPlanSeatFixture(t *testing.T) (agentFixtureSet, []int64) {
	t.Helper()
	f := newAgentWorkflowFixture(t, func(bundle *config.Bundle) {
		workflow := &bundle.Workflows[0]
		workflow.Transitions = []config.Transition{{From: 1, To: 2}, {From: 2, To: 3}}
		workflow.Plans = config.WorkflowPlans{
			Complete: config.OperationPolicy{Guards: []config.TransitionGuard{{Type: domain.GuardPlanCommentsTagged, Tag: domain.AgentResultTag + "auditor"}}},
			Agents:   []config.AgentRoleConfig{{Key: "auditor", Command: "okt-start", Persona: "auditor", Brief: "check every task against the goal"}},
		}
	})
	if _, err := f.service.CreatePlan(f.ctx, contract.CreatePlanInput{Slug: "p", Name: "Release", GoalBody: "Ship the release safely."}); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	wave, err := f.service.AddPlanWave(f.ctx, contract.AddPlanWaveInput{Slug: "p", Name: "build"})
	if err != nil {
		t.Fatalf("AddPlanWave() error = %v", err)
	}
	second, err := f.service.CreateTask(f.ctx, contract.CreateTaskInput{Title: "Write the notes", Description: "Release notes for the plan", SkipSimilarityCheck: true})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	tasks := []int64{f.task.ID, second.Task.ID}
	for _, id := range tasks {
		if _, err := f.service.AssignPlanTask(f.ctx, contract.AssignPlanTaskInput{TaskID: id, Slug: "p", WaveID: wave.Wave.ID}); err != nil {
			t.Fatalf("AssignPlanTask(%d) error = %v", id, err)
		}
		if _, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: id, BucketKey: "dev"}); err != nil {
			t.Fatalf("MoveTask(%d) error = %v", id, err)
		}
	}
	return f, tasks
}

// heldPlanSeat moves the plan's first task to done, has the complete gate
// hold the second, and starts the auditor seat as the gate's hook does.
func heldPlanSeat(t *testing.T) (agentFixtureSet, []int64, int64, domain.TeamOutcome) {
	t.Helper()
	f, tasks := newPlanSeatFixture(t)
	if _, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: tasks[0], BucketKey: "done"}); err != nil {
		t.Fatalf("first move to done error = %v", err)
	}
	_, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: tasks[1], BucketKey: "done"})
	assertCodedError(t, err, domain.ErrGuardViolation)
	plan, err := f.service.ShowPlan(f.ctx, contract.ShowPlanInput{Slug: "p"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := f.service.SpawnSeat(f.ctx, contract.SpawnSeatInput{PlanID: plan.Plan.ID, Member: "auditor"})
	if err != nil {
		t.Fatalf("SpawnSeat(auditor) error = %v", err)
	}
	return f, tasks, plan.Plan.ID, started.Team
}

// TestPlanSeatStartsOncePerPlan verifies a plan seat starts in the plan's
// team with the plan in its prompt, a repeated start leaves it as it is,
// and an explicit spawn runs it again.
func TestPlanSeatStartsOncePerPlan(t *testing.T) {
	f, _, planID, started := heldPlanSeat(t)
	seat := started.Members[0]
	if started.Status != domain.TeamStarted || started.PlanID != planID || started.TaskID != 0 || seat.Persona != "auditor" {
		t.Fatalf("plan seat = %+v, want auditor started for the plan on persona auditor", started)
	}
	assertPromptContains(t, seat.SessionID, "## Persona — Auditor", "plan `p` \"Release\"", "Ship the release safely.", "1. build: 1 of 2 done",
		"\"Write the notes\" in `dev`", "check every task against the goal", "okt comment add --scope plan --plan p -a agent -T review-auditor")
	again, err := f.service.SpawnSeat(f.ctx, contract.SpawnSeatInput{PlanID: planID, Member: "auditor"})
	if err != nil || again.Team.Status != domain.TeamRunning || again.Team.TeamID != started.TeamID || len(f.agents.starts) != 1 {
		t.Fatalf("second SpawnSeat = %+v, %v (starts %d); want the seat left as it is", again.Team, err, len(f.agents.starts))
	}
	if _, err := f.service.SpawnSeat(f.ctx, contract.SpawnSeatInput{PlanID: planID, Member: "security"}); err == nil {
		t.Fatal("SpawnSeat started a key that is no plan seat")
	}
	rerun, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{Plan: "p", Member: "auditor"})
	if err != nil || rerun.Team.TeamID != started.TeamID || rerun.Team.Members[0].SessionID == seat.SessionID {
		t.Fatalf("SpawnAgents(--plan) = %+v, %v; want a new session in the plan's team", rerun.Team, err)
	}
}

// TestPlanSeatResultLetsThePlanComplete verifies a finished plan seat
// records team.finished on the plan, is missing its result until it
// publishes review-<key> on the plan, and that the result lets the held
// move complete the plan, which then starts no seat.
func TestPlanSeatResultLetsThePlanComplete(t *testing.T) {
	f, tasks, _, started := heldPlanSeat(t)
	if session := f.run(t, started.Members[0].SessionID, 0, "true"); session.Status != domain.SessionDone {
		t.Fatalf("auditor run = %+v, want done", session)
	}
	finished := planEvents(t, f, domain.EventTypeTeamFinished)
	if len(finished) != 1 || !strings.Contains(finished[0].Payload, `"plan_id":`) || finished[0].EntityType != domain.EventEntityPlan {
		t.Fatalf("team.finished = %+v, want one on the plan", finished)
	}
	shown, err := f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: started.TeamID})
	if err != nil || len(shown.MissingResults) != 1 || shown.Follow[0] != "okt plan continue p" {
		t.Fatalf("ShowTeam() = %+v, %v; want the auditor missing its result", shown, err)
	}
	if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{Scope: domain.CommentScopePlan, PlanSlug: "p", Body: "audited", Tags: []string{"review-auditor"}}); err != nil {
		t.Fatal(err)
	}
	shown, err = f.service.ShowTeam(f.ctx, contract.ShowTeamInput{TeamID: started.TeamID})
	if err != nil || len(shown.Results) != 1 || shown.Results[0].Member != "auditor" || len(shown.MissingResults) != 0 {
		t.Fatalf("ShowTeam() after the result = %+v, %v", shown, err)
	}
	if _, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: tasks[1], BucketKey: "done"}); err != nil {
		t.Fatalf("last move after the audit error = %v", err)
	}
	if _, err := f.service.SpawnAgents(f.ctx, contract.SpawnAgentsInput{Plan: "p", Member: "auditor"}); err == nil {
		t.Fatal("SpawnAgents(--plan) ran a seat of a done plan")
	}
}

// planEvents returns the project's events of eventType recorded on a plan.
func planEvents(t *testing.T, f agentFixtureSet, eventType string) []domain.EventRow {
	t.Helper()
	events, err := f.store.ListEvents(f.ctx, domain.EventFilter{ProjectID: f.project.ID})
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.EventRow
	for _, event := range events {
		if event.EventType == eventType {
			out = append(out, event)
		}
	}
	return out
}

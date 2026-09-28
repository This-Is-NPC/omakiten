package tui

import (
	"errors"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
)

// TestViewChangeReturnsRefreshCmdAndDefersFold pins the async refresh
// contract: navigating away from a sub that ran refresh() used to call
// refreshCurrentView synchronously on the Update goroutine. After
// perf/tui-refresh-async the nav handler returns a tea.Cmd whose msg
// is folded on the next Update. The test reproduces a board-to-table
// nav and asserts (a) Update returns a non-nil cmd, (b) the cmd's msg
// is a refreshAfterViewChangeMsg with valid snap data, and (c) feeding
// the msg back through Update populates the entity slices.
func TestViewChangeReturnsRefreshCmdAndDefersFold(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.navigation = screenhost.TasksBoard

	// Drive a board → table nav via the sub-cycle key. Update returns
	// immediately with the heavy refresh captured inside the returned
	// tea.Cmd — the previous board view stays rendered until the worker's
	// msg lands.
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	got := updated.(Model)
	if got.navigation != screenhost.TasksTable {
		t.Fatalf("after / nav, sub = %v, want screenhost.TasksTable (board→table)", got.navigation)
	}
	if cmd == nil {
		t.Fatalf("Update(/) returned nil cmd, want async refresh cmd; got.navigation=%v", got.navigation)
	}
	msg := cmd()
	rmsg, ok := msg.(refreshAfterViewChangeMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want refreshAfterViewChangeMsg", msg)
	}
	if rmsg.err != nil {
		t.Fatalf("refreshAfterViewChangeMsg.err = %v", rmsg.err)
	}
	if !rmsg.snapValid {
		t.Fatalf("refreshAfterViewChangeMsg.snapValid = false; worker did not populate snap")
	}
	if rmsg.projectID != model.project.ID || rmsg.projectGeneration != model.projectGeneration {
		t.Fatalf("refreshAfterViewChangeMsg scope = (%d, %d), want (%d, %d)", rmsg.projectID, rmsg.projectGeneration, model.project.ID, model.projectGeneration)
	}
	if rmsg.viewChangeGeneration != got.viewChangeGeneration {
		t.Fatalf("refreshAfterViewChangeMsg view generation = %d, want %d", rmsg.viewChangeGeneration, got.viewChangeGeneration)
	}

	// Fold the worker's msg back into the model.
	folded, _ := got.Update(rmsg)
	final := folded.(Model)
	if len(final.skills) == 0 {
		t.Fatalf("post-fold skills = empty, want the snapshot-sourced skill slice")
	}
}

func TestViewChangeRefreshCapturesProjectGenerationBeforeWorkerRuns(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.projectGeneration = 4
	cmd := model.refreshHeavyAfterViewChangeCmd()
	if cmd == nil {
		t.Fatal("refreshHeavyAfterViewChangeCmd() returned nil")
	}
	model.projectGeneration = 5
	msg, ok := cmd().(refreshAfterViewChangeMsg)
	if !ok {
		t.Fatalf("refresh command returned %T, want refreshAfterViewChangeMsg", msg)
	}
	if msg.projectID != model.project.ID || msg.projectGeneration != 4 {
		t.Fatalf("captured scope = (%d, %d), want (%d, 4)", msg.projectID, msg.projectGeneration, model.project.ID)
	}
	if msg.viewChangeGeneration != 1 {
		t.Fatalf("captured view generation = %d, want 1", msg.viewChangeGeneration)
	}
	model.applyRefreshAfterViewChange(msg)
}

func TestViewChangeRefreshDropsDelayedResultAfterProjectSwitch(t *testing.T) {
	cases := map[string]struct {
		err error
	}{
		"success": {},
		"failure": {err: errors.New("project A unavailable")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			model := buildRefreshHotPathModel(t)
			model.project = domain.ProjectContext{ID: 1}
			model.projectGeneration = 4
			model.viewChangeGeneration = 4
			setViewChangeState(&model, 2, "project B")
			model.project = domain.ProjectContext{ID: 2}
			model.projectGeneration = 5
			model.viewChangeGeneration = 5
			before := viewChangeState{model: model}

			stale := refreshAfterViewChangeMsg{
				projectID:            1,
				projectGeneration:    4,
				viewChangeGeneration: 4,
				snap:                 viewChangeSnapshot(1, "project A"),
				snapValid:            tc.err == nil,
				plans:                []domain.PlanRollup{{Plan: domain.Plan{ID: 1, Slug: "project-a"}}},
				plansValid:           tc.err == nil,
				err:                  tc.err,
			}
			updated, _ := model.Update(stale)
			got := updated.(Model)

			before.assertUnchanged(t, got)
		})
	}
}

func TestViewChangeRefreshDropsOldVisitAfterProjectSwitchBack(t *testing.T) {
	cases := map[string]struct {
		err error
	}{
		"success": {},
		"failure": {err: errors.New("old project A unavailable")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			model := buildRefreshHotPathModel(t)
			model.project = domain.ProjectContext{ID: 1}
			model.projectGeneration = 7
			model.viewChangeGeneration = 7
			stale := refreshAfterViewChangeMsg{
				projectID:            1,
				projectGeneration:    7,
				viewChangeGeneration: 7,
				snap:                 viewChangeSnapshot(1, "old project A"),
				snapValid:            tc.err == nil,
				plans:                []domain.PlanRollup{{Plan: domain.Plan{ID: 1, Slug: "old-project-a"}}},
				plansValid:           tc.err == nil,
				err:                  tc.err,
			}

			// A -> B -> A: the project ID is the same on the final visit, but
			// the generation proves that this result belongs to the old visit.
			model.project = domain.ProjectContext{ID: 2}
			model.projectGeneration = 8
			model.viewChangeGeneration = 8
			setViewChangeState(&model, 2, "project B")
			model.project = domain.ProjectContext{ID: 1}
			model.projectGeneration = 9
			model.viewChangeGeneration = 9
			setViewChangeState(&model, 1, "current project A")
			before := viewChangeState{model: model}

			updated, _ := model.Update(stale)
			got := updated.(Model)
			before.assertUnchanged(t, got)

			current := refreshAfterViewChangeMsg{
				projectID:            1,
				projectGeneration:    9,
				viewChangeGeneration: 9,
				snap:                 viewChangeSnapshot(1, "fresh project A"),
				snapValid:            true,
				plans:                []domain.PlanRollup{{Plan: domain.Plan{ID: 9, Slug: "fresh-project-a"}}},
				plansValid:           true,
			}
			updated, _ = got.Update(current)
			got = updated.(Model)
			if got.tasks[0].Title != "fresh project A" || got.plansScreen.Rollups()[0].Plan.Slug != "fresh-project-a" {
				t.Fatalf("current A result was not applied: tasks=%+v plans=%+v", got.tasks, got.plansScreen.Rollups())
			}
		})
	}
}

func TestViewChangeRefreshDropsOlderSameProjectRequest(t *testing.T) {
	cases := map[string]struct {
		err error
	}{
		"success": {},
		"failure": {err: errors.New("old navigation unavailable")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			model := buildRefreshHotPathModel(t)
			model.project = domain.ProjectContext{ID: 1}
			model.projectGeneration = 3
			model.viewChangeGeneration = 2
			setViewChangeState(&model, 1, "current view")
			before := viewChangeState{model: model}

			stale := refreshAfterViewChangeMsg{
				projectID:            1,
				projectGeneration:    3,
				viewChangeGeneration: 1,
				snap:                 viewChangeSnapshot(1, "old navigation"),
				snapValid:            tc.err == nil,
				plans:                []domain.PlanRollup{{Plan: domain.Plan{ID: 1, Slug: "old-navigation"}}},
				plansValid:           tc.err == nil,
				err:                  tc.err,
			}
			updated, _ := model.Update(stale)
			before.assertUnchanged(t, updated.(Model))
		})
	}
}

func TestViewChangeRefreshDropsResultAfterRuntimeRotation(t *testing.T) {
	for name, tc := range map[string]struct {
		err error
	}{
		"success": {},
		"failure": {err: errors.New("old runtime unavailable")},
	} {
		for scenario, switchProject := range map[string]bool{
			"same project":   false,
			"project switch": true,
		} {
			t.Run(name+"/"+scenario, func(t *testing.T) {
				t.Parallel()
				model := buildRefreshHotPathModel(t)
				model.project = domain.ProjectContext{ID: 1}
				model.projectGeneration = 4
				model.viewChangeGeneration = 4
				model.studioRuntimeGeneration = 7
				cmd := model.refreshHeavyAfterViewChangeCmd()
				result := cmd().(refreshAfterViewChangeMsg)
				if result.runtimeGeneration != 7 {
					t.Fatalf("captured runtime generation = %d, want 7", result.runtimeGeneration)
				}
				if tc.err != nil {
					result.err = tc.err
					result.snapValid = false
					result.plansValid = false
				}

				// A config hot-reload rotates the runtime while the worker is queued.
				model.studioRuntimeGeneration = 8
				if switchProject {
					model.project = domain.ProjectContext{ID: 2}
					model.projectGeneration = 5
					model.viewChangeGeneration = 5
				}
				setViewChangeState(&model, model.project.ID, "current runtime")
				before := viewChangeState{model: model}

				updated, _ := model.Update(result)
				before.assertUnchanged(t, updated.(Model))
			})
		}
	}
}

func TestViewChangeRefreshDropsOldRuntimeAfterProjectRoundTrip(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	model.project = domain.ProjectContext{ID: 1}
	model.projectGeneration = 4
	model.viewChangeGeneration = 4
	model.studioRuntimeGeneration = 7
	cmd := model.refreshHeavyAfterViewChangeCmd()
	result := cmd().(refreshAfterViewChangeMsg)

	model.project = domain.ProjectContext{ID: 2}
	model.projectGeneration = 5
	model.viewChangeGeneration = 5
	model.studioRuntimeGeneration = 8
	model.project = domain.ProjectContext{ID: 1}
	model.projectGeneration = 6
	model.viewChangeGeneration = 6
	setViewChangeState(&model, 1, "new runtime project A")
	before := viewChangeState{model: model}

	updated, _ := model.Update(result)
	before.assertUnchanged(t, updated.(Model))
}

func viewChangeSnapshot(projectID int64, title string) contract.BoardSnapshot {
	return contract.BoardSnapshot{
		Tasks:        []domain.Task{{ID: projectID, ProjectID: projectID, Title: title}},
		Workflow:     domain.Workflow{ID: projectID, Key: title},
		Dependencies: []domain.TaskDependency{{TaskID: projectID}},
		Comments:     []domain.Comment{{ID: projectID, ProjectID: projectID, Body: title}},
		Laws:         []domain.Law{{ID: projectID, Key: title, Body: title, Active: true}},
		Skills:       []domain.Skill{{ID: projectID, Key: title}},
		Personas:     []domain.Persona{{ID: projectID, Key: title}},
		AllTags:      []domain.Tag{{ID: projectID, Name: title}},
		TaskTagsByID: map[int64][]domain.Tag{projectID: {{ID: projectID, Name: title}}},
	}
}

func setViewChangeState(model *Model, projectID int64, marker string) {
	snapshot := viewChangeSnapshot(projectID, marker)
	model.tasks = snapshot.Tasks
	model.workflow = snapshot.Workflow
	model.dependencies = snapshot.Dependencies
	model.comments = snapshot.Comments
	model.laws = snapshot.Laws
	model.skills = snapshot.Skills
	model.personas = snapshot.Personas
	model.tags = snapshot.AllTags
	model.taskTagsMap = snapshot.TaskTagsByID
	model.plansScreen = model.plansScreen.Apply([]domain.PlanRollup{{Plan: domain.Plan{ID: projectID, Slug: marker}}}, nil)
	model.status = marker + " status"
}

type viewChangeState struct {
	model Model
}

func (before viewChangeState) assertUnchanged(t *testing.T, got Model) {
	t.Helper()
	if got.status != before.model.status ||
		!sameTasks(got.tasks, before.model.tasks) ||
		!sameWorkflow(got.workflow, before.model.workflow) ||
		!sameDependencies(got.dependencies, before.model.dependencies) ||
		!sameComments(got.comments, before.model.comments) ||
		!sameLaws(got.laws, before.model.laws) ||
		!sameSkills(got.skills, before.model.skills) ||
		!samePersonas(got.personas, before.model.personas) ||
		!sameTags(got.tags, before.model.tags) ||
		!sameTaskTags(got.taskTagsMap, before.model.taskTagsMap) ||
		!reflect.DeepEqual(got.languages, before.model.languages) ||
		!reflect.DeepEqual(got.metrics, before.model.metrics) ||
		!sameRollups(got.plansScreen.Rollups(), before.model.plansScreen.Rollups()) {
		t.Fatalf("stale view-change result mutated project state: status=%q tasks=%+v workflow=%+v comments=%+v plans=%+v", got.status, got.tasks, got.workflow, got.comments, got.plansScreen.Rollups())
	}
}

func sameTasks(a, b []domain.Task) bool                  { return equalValue(a, b) }
func sameWorkflow(a, b domain.Workflow) bool             { return equalValue(a, b) }
func sameDependencies(a, b []domain.TaskDependency) bool { return equalValue(a, b) }
func sameComments(a, b []domain.Comment) bool            { return equalValue(a, b) }
func sameLaws(a, b []domain.Law) bool                    { return equalValue(a, b) }
func sameSkills(a, b []domain.Skill) bool                { return equalValue(a, b) }
func samePersonas(a, b []domain.Persona) bool            { return equalValue(a, b) }
func sameTags(a, b []domain.Tag) bool                    { return equalValue(a, b) }
func sameTaskTags(a, b map[int64][]domain.Tag) bool      { return equalValue(a, b) }
func sameRollups(a, b []domain.PlanRollup) bool          { return equalValue(a, b) }

func equalValue[T any](a, b T) bool {
	return reflect.DeepEqual(a, b)
}

package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/commentdetail"
	"omakiten/internal/tui/screens/description"
	"omakiten/internal/tui/screens/entitydetail"
	"omakiten/internal/tui/screens/entitylist"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/plans"
	"omakiten/internal/tui/screens/table"
	"omakiten/internal/tui/screens/taskdetail"
)

func TestHostConsumesTaskScreenOutcomes(t *testing.T) {
	task := domain.Task{ID: 1980, Title: "Extract Table and Graph screens", BucketKey: "dev", Priority: 2}
	m := Model{
		styles:      newStyles(config.Theme{}),
		width:       120,
		height:      40,
		tasks:       []domain.Task{task},
		priorities:  []config.PriorityDefinition{{ID: 2, Value: "normal"}},
		tableScreen: table.New(),
	}

	m.applyScreenOutcome(screenhost.OpenTask(m.boundTableScreen(), task.ID, nil))
	if !m.inTaskDetail() || m.taskDetailScreen.Payload().Task.ID != task.ID {
		t.Fatalf("open outcome left task detail = %v/%d", m.screenStack, m.taskDetailScreen.Payload().Task.ID)
	}

	m.closeTaskScreen("")
	m.applyScreenOutcome(screenhost.MoveTask(m.boundTableScreen(), task.ID, nil))
	if m.mode != modeMove || m.moveInputTargetID != task.ID {
		t.Fatalf("move outcome left mode/target = %v/%d", m.mode, m.moveInputTargetID)
	}
}

func TestUpdateDispatchesActiveRouteScreens(t *testing.T) {
	tests := []struct {
		name   string
		model  Model
		assert func(*testing.T, Model)
	}{
		{
			name: "table",
			model: Model{
				styles: newStyles(config.Theme{}), width: 100, height: 30,
				top: topTasks, sub: subTable, tableScreen: table.New(),
				tasks: []domain.Task{{ID: 1, Title: "first"}, {ID: 2, Title: "second"}},
			},
			assert: func(t *testing.T, got Model) {
				if got.tableScreen.Selected() != 1 {
					t.Fatalf("selected row = %d, want 1", got.tableScreen.Selected())
				}
			},
		},
		{
			name: "plans",
			model: Model{
				styles: newStyles(config.Theme{}), width: 100, height: 30,
				top: topTasks, sub: subPlans,
				plansScreen: plans.New().Apply([]domain.PlanRollup{
					{Plan: domain.Plan{ID: 1, Slug: "first"}},
					{Plan: domain.Plan{ID: 2, Slug: "second"}},
				}, nil),
			},
			assert: func(t *testing.T, got Model) {
				if got.plansScreen.Cursor() != 1 {
					t.Fatalf("plan cursor = %d, want 1", got.plansScreen.Cursor())
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next, _ := tc.model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
			tc.assert(t, next.(Model))
		})
	}
}

type failingUpdateCommentStore struct{ *snapstore.Store }

func (failingUpdateCommentStore) EditComment(context.Context, int64, int64, domain.CommentEdit) (domain.Comment, domain.Event, error) {
	return domain.Comment{}, domain.Event{}, errors.New("save failed")
}

func TestCommentSaveFailurePreservesDirtyScreenBuffer(t *testing.T) {
	m, _, task := scopedFeedModel(t)
	var comment domain.Comment
	for _, candidate := range m.comments {
		if candidate.TaskID == task.ID {
			comment = candidate
			break
		}
	}
	if comment.ID == 0 {
		t.Fatal("setup: task comment not loaded")
	}
	m.commentDetailScreen = m.boundCommentDetailScreen().Open(commentdetail.Payload{Comment: comment, Editable: true})
	m.screenStack = []screenhost.ID{screenhost.TaskDetail, screenhost.CommentDetail}
	store, ok := m.repos.Comments.(*snapstore.Store)
	if !ok {
		t.Fatalf("Comments is %T, want *snapstore.Store", m.repos.Comments)
	}
	failing := failingUpdateCommentStore{Store: store}
	svc := operation.NewService(failing, operation.ProjectSelector{ProjectID: m.project.ID})
	svc.SetSnapshot(store.Snapshot())
	if err := m.repos.Cache.Install(m.repos.ProjectID, &agentruntime.ProjectRuntime{Snapshot: store.Snapshot(), Service: svc}); err != nil {
		t.Fatalf("Install failing comment store: %v", err)
	}
	m = pressRune(t, m, 'e')
	m = pressRune(t, m, 'x')
	want := m.commentDetailScreen.Value()
	m = pressKey(t, m, tea.KeyCtrlS)
	if m.commentDetailScreen.Mode() != commentdetail.ModeEdit || !m.commentDetailScreen.Dirty() || m.commentDetailScreen.Value() != want {
		t.Fatalf("failed save state = mode:%v dirty:%v value:%q want:%q", m.commentDetailScreen.Mode(), m.commentDetailScreen.Dirty(), m.commentDetailScreen.Value(), want)
	}
	if !strings.Contains(m.status, "save failed") || !strings.Contains(stripANSI(m.View()), "save failed") {
		t.Fatalf("save failure not surfaced: status=%q view=%q", m.status, stripANSI(m.View()))
	}
}

func TestCommentEditQuestionMarkTypesInsteadOfOpeningGlobalHelp(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{}), width: 80, height: 24}
	m.commentDetailScreen = commentdetail.New().Open(commentdetail.Payload{Comment: domain.Comment{ID: 1, Body: "body"}, Editable: true})
	m.screenStack = []screenhost.ID{screenhost.CommentDetail}
	m = pressRune(t, m, 'e')
	m = pressRune(t, m, '?')
	if m.helpOpen {
		t.Fatal("global help opened while Comment edit owned text input")
	}
	if !strings.HasSuffix(m.commentDetailScreen.Value(), "?") {
		t.Fatalf("comment edit value = %q, want literal question mark", m.commentDetailScreen.Value())
	}
}

func TestDescriptionAndCommentAreRegisteredHostedRouteStackScreens(t *testing.T) {
	for _, id := range []screenhost.ID{screenhost.TaskDescription, screenhost.CommentDetail} {
		if _, ok := screenRegistry.ByID(id); !ok {
			t.Fatalf("screen %q is not registered", id)
		}
	}
	task := domain.Task{ID: 42, Title: "reader", Description: "body", BucketKey: "dev"}
	comment := domain.Comment{ID: 7, TaskID: task.ID, Scope: domain.CommentScopeTask, Body: "comment"}
	m := Model{
		styles: newStyles(config.Theme{}), width: 100, height: 30, tasks: []domain.Task{task},
		descriptionReaderScreen: description.New(), commentDetailScreen: commentdetail.New(),
	}
	m.openDescriptionScreen(task)
	if got := m.screenStack; len(got) != 1 || got[0] != screenhost.TaskDescription {
		t.Fatalf("description stack = %v", got)
	}
	if _, ok := m.hostedScreen(screenhost.TaskDescription); !ok {
		t.Fatal("description screen is not hosted")
	}
	m.popScreen()
	m.openCommentScreen(comment, false)
	if got := m.screenStack; len(got) != 1 || got[0] != screenhost.CommentDetail {
		t.Fatalf("comment stack = %v", got)
	}
	if payload := m.commentDetailScreen.Payload(); payload.Comment.ID != comment.ID || !payload.Editable {
		t.Fatalf("comment payload = %+v", payload)
	}
}

func TestTaskDetailIsRegisteredAndHosted(t *testing.T) {
	descriptor, ok := screenRegistry.ByID(screenhost.TaskDetail)
	if !ok {
		t.Fatal("Task Detail is not registered")
	}
	if descriptor.Reload != screenhost.ReloadTaskActivity {
		t.Fatalf("reload policy = %v, want task activity", descriptor.Reload)
	}

	m := Model{taskDetailScreen: taskdetail.New()}
	screen, ok := m.hostedScreen(screenhost.TaskDetail)
	if !ok {
		t.Fatal("Task Detail is not hosted")
	}
	if _, ok := screen.(taskdetail.Screen); !ok {
		t.Fatalf("hosted screen = %T, want taskdetail.Screen", screen)
	}
}

func TestTaskDetailMoveDoesNotClaimStandaloneReaderInput(t *testing.T) {
	payload := taskdetail.Payload{Generation: 1, Task: domain.Task{ID: 10, Title: "detail"}}
	m := Model{
		styles:              newStyles(config.Theme{}),
		width:               100,
		height:              30,
		taskDetailScreen:    taskdetail.New().Open(payload, screenhost.NewFrame(screenhost.FrameOptions{Width: 100, Height: 30})),
		screenStack:         []screenhost.ID{screenhost.TaskDetail},
		commentDetailScreen: commentdetail.New().Open(commentdetail.Payload{Comment: domain.Comment{ID: 9, Body: "reader"}}),
	}

	out := m.taskDetailScreen.Update(m.screenFrame(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.storeScreen(out.Screen)
	if m.taskDetailScreen.State().Mode != taskdetail.ModeMove {
		t.Fatal("detail move mode was not screen-owned")
	}
	if m.mode != modeNormal {
		t.Fatalf("detail move leaked into root input mode %v", m.mode)
	}
	if m.commentDetailScreen.Payload().Comment.ID != 9 {
		t.Fatal("standalone reader projection was consumed by detail move")
	}
}

func TestHostConsumesParameterizedEntityListOutcomes(t *testing.T) {
	cases := []struct {
		descriptor entitylist.Descriptor
		kind       entityKind
		slug       string
	}{
		{entitylist.Laws(), entityKindLaw, "scope"},
		{entitylist.Personas(), entityKindPersona, "builder"},
		{entitylist.Skills(), entityKindSkill, "testing"},
		{entitylist.Templates(), entityKindTemplate, "task-feature"},
	}
	for _, tc := range cases {
		t.Run(string(tc.descriptor.Kind), func(t *testing.T) {
			m := Model{styles: newStyles(config.Theme{}), width: 120, height: 40}
			m.applyScreenOutcome(screenhost.Outcome{Action: screenhost.Action{
				Kind: screenhost.ActionOpenEntity, EntityKind: string(tc.descriptor.Kind), Value: tc.slug,
			}})
			if len(m.screenStack) != 1 || m.screenStack[0] != screenhost.EntityDetail {
				t.Fatalf("detail route stack = %v", m.screenStack)
			}
			if payload := m.entityDetailScreen.Payload(); entityKindFromDetail(payload.Kind) != tc.kind || payload.Slug != tc.slug {
				t.Fatalf("detail payload = kind:%v slug:%q", payload.Kind, payload.Slug)
			}
		})
	}
}

func TestEntityListDeletePreparationDoesNotArmLegacyRootFuse(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	m.applyScreenOutcome(screenhost.Outcome{Action: screenhost.Action{
		Kind: screenhost.ActionPrepareEntityDelete, EntityKind: string(entitylist.KindLaws), Value: "scope",
	}})
	if m.deletePending {
		t.Fatal("parameterized list preparation armed the legacy root delete fuse")
	}
	if m.status == "" {
		t.Fatal("parameterized list preparation did not surface confirmation status")
	}
}

func TestEntityListEmptyOpenPreservesLegacyEmptyState(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	m.applyScreenOutcome(screenhost.Outcome{Action: screenhost.Action{
		Kind: screenhost.ActionOpenEntity, EntityKind: string(entitylist.KindSkills),
	}})
	if len(m.screenStack) != 0 {
		t.Fatalf("empty list opened detail route %v", m.screenStack)
	}
	if m.status == "" {
		t.Fatal("empty list did not surface the nothing-to-open status")
	}
}

func TestHostConsumesTemplateAndTagPolicyOutcomes(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{}), width: 120, height: 40}
	m.applyScreenOutcome(screenhost.Outcome{Action: screenhost.Action{Kind: screenhost.ActionTemplateCreateHint, EntityKind: string(entitylist.KindTemplates)}})
	if m.status == "" {
		t.Fatal("template create hint missing")
	}
	m.status = ""
	m.applyScreenOutcome(screenhost.Outcome{Action: screenhost.Action{Kind: screenhost.ActionTemplateDeleteHint, EntityKind: string(entitydetail.KindTemplate), Value: "task-feature"}})
	if m.status == "" {
		t.Fatal("template delete hint missing")
	}
}

func TestHostDisarmsTagConfirmationWhenPolicyRejectsInUseTag(t *testing.T) {
	frameModel := Model{
		styles: newStyles(config.Theme{}), width: 120, height: 40,
		tags: []domain.Tag{{Name: "in-use", Label: "In Use", UsageCount: 2}},
		entityListScreens: map[screenhost.ID]entitylist.Screen{
			screenhost.SettingsTags: entitylist.New(entitylist.Tags()).Bind([]entitylist.Item{{Slug: "in-use", Label: "In Use"}}, nil),
		},
	}
	armed := frameModel.entityListScreens[screenhost.SettingsTags].Update(frameModel.screenFrame(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	frameModel.applyScreenOutcome(armed)
	if frameModel.status == "" {
		t.Fatal("policy rejection status missing")
	}
	if got := len(frameModel.entityListScreens[screenhost.SettingsTags].Footer(frameModel.screenFrame())); got == 2 {
		t.Fatal("in-use tag remained armed after host policy rejection")
	}
}

func TestHostRechecksTagPolicyAtConfirmation(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{}), tags: []domain.Tag{{Name: "changed", Label: "Changed", UsageCount: 1}}}
	m.applyScreenOutcome(screenhost.Outcome{Action: screenhost.Action{Kind: screenhost.ActionDeleteTag, Value: "changed"}})
	if !strings.Contains(strings.ToLower(m.status), "use") {
		t.Fatalf("confirmation bypassed tag usage policy: %q", m.status)
	}
}

func TestRefreshEntityDetailProjectionUsesLatestBundleProjection(t *testing.T) {
	m := Model{
		styles: newStyles(config.Theme{}), width: 120, height: 40,
		laws:               []domain.Law{{Key: "scope", Body: "updated body"}},
		entityDetailScreen: entitydetail.New().Open(entitydetail.Payload{Kind: entitydetail.KindLaw, Slug: "scope", Body: "stale body"}),
		screenStack:        []screenhost.ID{screenhost.EntityDetail},
	}
	m.refreshEntityDetailProjection()
	if got := m.entityDetailScreen.Payload().Body; got != "updated body" {
		t.Fatalf("refreshed detail body = %q", got)
	}
}

func TestPlanNetworkResultsRejectStaleGenerationScopeAndCancellation(t *testing.T) {
	alpha := plannetwork.Payload{Show: domain.PlanShow{Plan: domain.Plan{ID: 1, Slug: "alpha", GoalBody: "current"}}}
	beta := plannetwork.Payload{Show: domain.PlanShow{Plan: domain.Plan{ID: 2, Slug: "beta", GoalBody: "stale"}}}
	m := Model{planNetworkScreen: plannetwork.New().Open(alpha), planNetworkGeneration: 3, screenStack: []screenhost.ID{screenhost.PlanNetwork}}

	m.applyPlanNetworkResult(planNetworkResultMsg{generation: 2, slug: "alpha", payload: plannetwork.Payload{Show: domain.PlanShow{Plan: domain.Plan{ID: 1, Slug: "alpha", GoalBody: "older"}}}})
	if got := m.planNetworkScreen.Show().Plan.GoalBody; got != "current" {
		t.Fatalf("stale generation applied: %q", got)
	}

	m.applyPlanNetworkResult(planNetworkResultMsg{generation: 3, slug: "beta", payload: beta})
	if got := m.planNetworkScreen.Show().Plan.Slug; got != "alpha" {
		t.Fatalf("stale scope applied: %q", got)
	}

	m.screenStack = nil
	m.applyPlanNetworkResult(planNetworkResultMsg{generation: 3, slug: "alpha", payload: plannetwork.Payload{Show: domain.PlanShow{Plan: domain.Plan{ID: 1, Slug: "alpha", GoalBody: "after-cancel"}}}})
	if got := m.planNetworkScreen.Show().Plan.GoalBody; got != "current" {
		t.Fatalf("cancelled route accepted result: %q", got)
	}
}

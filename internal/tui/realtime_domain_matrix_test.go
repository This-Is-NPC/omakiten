package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

type scriptedWatermark struct {
	version  atomic.Int64
	calls    atomic.Int64
	failNext atomic.Bool
}

func newScriptedWatermark(version int64) *scriptedWatermark {
	w := &scriptedWatermark{}
	w.version.Store(version)
	return w
}

func (w *scriptedWatermark) DataVersion(context.Context) (int64, error) {
	w.calls.Add(1)
	if w.failNext.CompareAndSwap(true, false) {
		return 0, errors.New("transient watermark probe failure")
	}
	return w.version.Load(), nil
}

type countingEventRepo struct {
	app.EventRepository
	listTaskActivityCalls atomic.Int64
	listEventsCalls       atomic.Int64
	eventCountCalls       atomic.Int64
	failNextTaskActivity  atomic.Bool
	failNextListEvents    atomic.Bool
	failNextEventCounts   atomic.Bool
}

func (r *countingEventRepo) ListTaskActivity(ctx context.Context, projectID, taskID int64, order string) ([]domain.Event, error) {
	r.listTaskActivityCalls.Add(1)
	if r.failNextTaskActivity.CompareAndSwap(true, false) {
		return nil, errors.New("transient activity query failure")
	}
	return r.EventRepository.ListTaskActivity(ctx, projectID, taskID, order)
}

func (r *countingEventRepo) ListEvents(ctx context.Context, filter domain.EventFilter) ([]domain.EventRow, error) {
	r.listEventsCalls.Add(1)
	if r.failNextListEvents.CompareAndSwap(true, false) {
		return nil, errors.New("transient logs query failure")
	}
	return r.EventRepository.ListEvents(ctx, filter)
}

func (r *countingEventRepo) EventCategoryCounts(ctx context.Context, projectID int64, since time.Time) (map[domain.EventCategory]int, error) {
	r.eventCountCalls.Add(1)
	if r.failNextEventCounts.CompareAndSwap(true, false) {
		return nil, errors.New("transient logs counts failure")
	}
	return r.EventRepository.EventCategoryCounts(ctx, projectID, since)
}

type countingMetricsRepo struct {
	calls    atomic.Int64
	failNext atomic.Bool
}

func (r *countingMetricsRepo) AgentMetricsSummary(context.Context, string, int64) ([]domain.AgentMetrics, string, error) {
	calls := r.calls.Add(1)
	if r.failNext.CompareAndSwap(true, false) {
		return nil, "", errors.New("transient metrics query failure")
	}
	return []domain.AgentMetrics{{
		AgentModel: "matrix-agent",
		Buckets: map[domain.EventMetricBucket]int{
			domain.MetricBucketErrorRecorded: int(calls),
		},
	}}, "2026-06-18", nil
}

type countingPlanRepo struct {
	app.PlanRepository
	getBySlugCalls           atomic.Int64
	listProjectPlanTaskCalls atomic.Int64
	failNextShow             atomic.Bool
}

func (r *countingPlanRepo) GetPlanBySlug(ctx context.Context, projectID int64, slug string) (domain.Plan, error) {
	r.getBySlugCalls.Add(1)
	if r.failNextShow.CompareAndSwap(true, false) {
		return domain.Plan{}, errors.New("transient plan show failure")
	}
	return r.PlanRepository.GetPlanBySlug(ctx, projectID, slug)
}

func (r *countingPlanRepo) ListProjectPlanTasks(ctx context.Context, projectID int64, buckets domain.BucketResolver) ([]domain.ProjectPlanTaskRow, error) {
	r.listProjectPlanTaskCalls.Add(1)
	return r.PlanRepository.ListProjectPlanTasks(ctx, projectID, buckets)
}

type realtimeMatrixFixture struct {
	ctx       context.Context
	store     *snapstore.Store
	project   domain.Project
	task      domain.Task
	plan      domain.Plan
	wave      domain.PlanWave
	planTask  domain.Task
	tasks     *countingTaskRepo
	events    *countingEventRepo
	plans     *countingPlanRepo
	metrics   *countingMetricsRepo
	watermark *scriptedWatermark
}

func newRealtimeMatrixFixture(t *testing.T) (*realtimeMatrixFixture, Model) {
	t.Helper()
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()
	task, err := store.CreateTask(ctx, project.ID, "matrix-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask(matrix-task) error = %v", err)
	}
	plan, err := store.CreatePlan(ctx, project.ID, "matrix-plan", "Matrix Plan", "initial goal")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Wave", 1)
	if err != nil {
		t.Fatalf("AddPlanWave() error = %v", err)
	}
	planTask, err := store.CreateTask(ctx, project.ID, "matrix-plan-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask(plan task) error = %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, planTask.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan() error = %v", err)
	}

	fixture := &realtimeMatrixFixture{
		ctx:       ctx,
		store:     store,
		project:   project,
		task:      task,
		plan:      plan,
		wave:      wave,
		planTask:  planTask,
		tasks:     &countingTaskRepo{TaskRepository: store},
		events:    &countingEventRepo{EventRepository: store},
		plans:     &countingPlanRepo{PlanRepository: store},
		metrics:   &countingMetricsRepo{},
		watermark: newScriptedWatermark(1),
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        fixture.tasks,
		Comments:     store,
		Dependencies: store,
		Events:       fixture.events,
		Metrics:      app.NewMetricsService(fixture.metrics),
		Plans:        fixture.plans,
		Watermark:    fixture.watermark,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height, model.width = 40, 160
	return fixture, model
}

func realtimeKindName(kind realtimeReloadKind) string {
	switch kind {
	case realtimeReloadBundle:
		return "bundle"
	case realtimeReloadActivity:
		return "activity"
	case realtimeReloadPlanShow:
		return "planShow"
	case realtimeReloadStats:
		return "stats"
	case realtimeReloadLogs:
		return "logs"
	default:
		return "none"
	}
}

func allRealtimeReloadDomains() []realtimeReloadKind {
	return []realtimeReloadKind{
		realtimeReloadBundle,
		realtimeReloadActivity,
		realtimeReloadPlanShow,
		realtimeReloadStats,
		realtimeReloadLogs,
	}
}

func openPlanNetworkFixture(t *testing.T, f *realtimeMatrixFixture, m *Model) {
	t.Helper()
	show, err := app.NewPlanServiceWithSnapshot(f.store, f.store.Snapshot()).Show(f.ctx, f.project.Context(), f.plan.Slug)
	if err != nil {
		t.Fatalf("PlanService.Show() error = %v", err)
	}
	m.taskScreen = taskScreenClosed
	m.top = topTasks
	m.sub = subPlans
	m.planNetworkOpen = true
	m.planNetworkShow = show
	m.invalidatePlanNetworkRowsCache()
	m.syncPlanNetworkScroll(m.planNetworkBuildRows())
}

func setRealtimeDomainView(t *testing.T, f *realtimeMatrixFixture, m *Model, kind realtimeReloadKind) {
	t.Helper()
	m.taskScreen = taskScreenClosed
	m.planNetworkOpen = false
	m.commentScreenOpen = false
	m.descriptionScreenOpen = false
	m.planGoalScreenOpen = false
	m.projectFormScreenOpen = false
	m.entityScreen = entityScreenClosed
	m.helpOpen = false
	m.paletteOpen = false
	m.mode = modeNormal
	m.moveMode = false
	switch kind {
	case realtimeReloadBundle:
		m.top = topTasks
		m.sub = subBoard
	case realtimeReloadActivity:
		m.openTaskView(f.task)
		m.applyTaskFocus(taskFocusActivity)
	case realtimeReloadPlanShow:
		openPlanNetworkFixture(t, f, m)
	case realtimeReloadStats:
		m.top = topStats
		m.sub = subStatsGeneral
	case realtimeReloadLogs:
		m.top = topStats
		m.sub = subStatsLogs
	default:
		t.Fatalf("unsupported realtime kind %v", kind)
	}
}

func collectRealtimeReloadCmds(t *testing.T, cmd tea.Cmd) []tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	if isRealtimeReloadCmd(cmd) {
		return []tea.Cmd{cmd}
	}
	msgC := make(chan tea.Msg, 1)
	go func() { msgC <- cmd() }()
	select {
	case msg := <-msgC:
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			return nil
		}
		var reloads []tea.Cmd
		for _, sub := range batch {
			if isRealtimeReloadCmd(sub) {
				reloads = append(reloads, sub)
			}
		}
		return reloads
	case <-time.After(25 * time.Millisecond):
		return nil
	}
}

func driveRealtimeTickAll(t *testing.T, m Model) (Model, []realtimeReloadMsg) {
	t.Helper()
	model, cmd := updateRealtimeTick(t, m)
	reloadCmds := collectRealtimeReloadCmds(t, cmd)
	msgs := make([]realtimeReloadMsg, 0, len(reloadCmds))
	for _, reload := range reloadCmds {
		msg := reload()
		rmsg, ok := msg.(realtimeReloadMsg)
		if !ok {
			t.Fatalf("realtime reload cmd produced %T, want realtimeReloadMsg", msg)
		}
		updated, foldCmd := model.Update(rmsg)
		if foldCmd != nil {
			t.Fatalf("Update(realtimeReloadMsg) returned unexpected cmd %T", foldCmd)
		}
		var okModel bool
		model, okModel = updated.(Model)
		if !okModel {
			t.Fatalf("Update(realtimeReloadMsg) returned %T, want Model", updated)
		}
		msgs = append(msgs, rmsg)
	}
	return model, msgs
}

func assertReloadKinds(t *testing.T, msgs []realtimeReloadMsg, want ...realtimeReloadKind) {
	t.Helper()
	if len(msgs) != len(want) {
		t.Fatalf("realtime reload count = %d (%v), want %d (%v)", len(msgs), reloadMsgKindNames(msgs), len(want), reloadKindNames(want))
	}
	for i, msg := range msgs {
		if msg.kind != want[i] {
			t.Fatalf("reload[%d] kind = %s, want %s (all=%v)", i, realtimeKindName(msg.kind), realtimeKindName(want[i]), reloadMsgKindNames(msgs))
		}
	}
}

func reloadMsgKindNames(msgs []realtimeReloadMsg) []string {
	names := make([]string, len(msgs))
	for i, msg := range msgs {
		names[i] = realtimeKindName(msg.kind)
	}
	return names
}

func reloadKindNames(kinds []realtimeReloadKind) []string {
	names := make([]string, len(kinds))
	for i, kind := range kinds {
		names[i] = realtimeKindName(kind)
	}
	return names
}

func assertRealtimeBaseline(t *testing.T, m Model, kind realtimeReloadKind, want int64) {
	t.Helper()
	version, ok := m.dataVersionBaseline(kind)
	if !ok || version != want {
		t.Fatalf("%s baseline = %d (ok=%v), want %d", realtimeKindName(kind), version, ok, want)
	}
}

func taskTitlePresent(tasks []domain.Task, title string) bool {
	for _, task := range tasks {
		if task.Title == title {
			return true
		}
	}
	return false
}

func validRealtimeReloadMsg(kind realtimeReloadKind, version int64) realtimeReloadMsg {
	msg := realtimeReloadMsg{kind: kind, dataVersion: version, dataVersionValid: true}
	switch kind {
	case realtimeReloadBundle:
		msg.snap = app.TUISnapshot{Tasks: []domain.Task{{ID: 1, Title: "bundle"}}}
		msg.snapValid = true
	case realtimeReloadActivity:
		msg.activity = []domain.Event{{ID: 1, Body: "activity"}}
		msg.activityForID = 1
		msg.activityValid = true
	case realtimeReloadPlanShow:
		msg.planShow = app.PlanShow{Plan: domain.Plan{ID: 1, Slug: "plan"}}
		msg.planValid = true
	case realtimeReloadStats:
		msg.statsSummary = domain.MetricsSummary{Period: "30d"}
		msg.statsValid = true
	case realtimeReloadLogs:
		msg.events = []domain.EventRow{{ID: 1, EventType: domain.EventTypeTaskCreated}}
		msg.eventCounts = map[domain.EventCategory]int{domain.EventCategoryTask: 1}
		msg.logsValid = true
	}
	return msg
}

func TestRealtimeTickScopedViewsCatchUpBundleMatrix(t *testing.T) {
	cases := []struct {
		name       string
		scopedKind realtimeReloadKind
		seedScoped func(t *testing.T, f *realtimeMatrixFixture)
	}{
		{
			name:       "task overlay activity",
			scopedKind: realtimeReloadActivity,
			seedScoped: func(t *testing.T, f *realtimeMatrixFixture) {
				t.Helper()
				if _, err := f.store.AddComment(f.ctx, f.project.ID, f.task.ID, "activity matrix comment", "human", nil); err != nil {
					t.Fatalf("AddComment() error = %v", err)
				}
			},
		},
		{
			name:       "stats summary",
			scopedKind: realtimeReloadStats,
			seedScoped: func(t *testing.T, f *realtimeMatrixFixture) {
				t.Helper()
				f.metrics.calls.Store(0)
			},
		},
		{
			name:       "logs inspector",
			scopedKind: realtimeReloadLogs,
			seedScoped: func(t *testing.T, f *realtimeMatrixFixture) {
				t.Helper()
				if err := f.store.RecordEntityEvent(f.ctx, "system", 0, f.project.ID, domain.EventTypeBundleSwapped, `{"matrix":"logs"}`); err != nil {
					t.Fatalf("RecordEntityEvent() error = %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, model := newRealtimeMatrixFixture(t)
			model.commitDataVersion(realtimeReloadBundle, 1)
			model.commitDataVersion(tc.scopedKind, 1)
			setRealtimeDomainView(t, f, &model, tc.scopedKind)

			boardTitle := fmt.Sprintf("bundle-catch-up-%s", realtimeKindName(tc.scopedKind))
			if _, err := f.store.CreateTask(f.ctx, f.project.ID, boardTitle, "", domain.Priority(2), "backlog", nil, f.store.Snapshot()); err != nil {
				t.Fatalf("CreateTask(%s) error = %v", boardTitle, err)
			}
			tc.seedScoped(t, f)
			f.watermark.version.Store(2)
			f.tasks.listCalls.Store(0)

			model, msgs := driveRealtimeTickAll(t, model)
			assertReloadKinds(t, msgs, tc.scopedKind)
			assertRealtimeBaseline(t, model, tc.scopedKind, 2)
			assertRealtimeBaseline(t, model, realtimeReloadBundle, 1)
			if got := f.tasks.listCalls.Load(); got != 0 {
				t.Fatalf("%s scoped tick rebuilt bundle ListTasks calls = %d, want 0", realtimeKindName(tc.scopedKind), got)
			}
			if taskTitlePresent(model.tasks, boardTitle) {
				t.Fatalf("%s scoped tick pulled bundle task %q before returning to board", realtimeKindName(tc.scopedKind), boardTitle)
			}

			setRealtimeDomainView(t, f, &model, realtimeReloadBundle)
			f.tasks.listCalls.Store(0)
			model, msgs = driveRealtimeTickAll(t, model)
			assertReloadKinds(t, msgs, realtimeReloadBundle)
			assertRealtimeBaseline(t, model, realtimeReloadBundle, 2)
			if got := f.tasks.listCalls.Load(); got == 0 {
				t.Fatalf("returning to board after %s did not reload bundle", realtimeKindName(tc.scopedKind))
			}
			if !taskTitlePresent(model.tasks, boardTitle) {
				t.Fatalf("board did not catch up after %s scoped tick; missing %q", realtimeKindName(tc.scopedKind), boardTitle)
			}
		})
	}
}

func TestRealtimeReloadNegativeIsolationEveryDomainPair(t *testing.T) {
	domains := allRealtimeReloadDomains()
	for _, consumed := range domains {
		for _, isolated := range domains {
			if consumed == isolated {
				continue
			}
			t.Run(realtimeKindName(consumed)+" does not advance "+realtimeKindName(isolated), func(t *testing.T) {
				var model Model
				model.commitDataVersion(isolated, 1)
				model.applyRealtimeReload(validRealtimeReloadMsg(consumed, 2))
				assertRealtimeBaseline(t, model, consumed, 2)
				assertRealtimeBaseline(t, model, isolated, 1)
			})
		}
	}
}

func TestRealtimeTickScopedFailedReloadAndProbeErrorNoAdvanceRetry(t *testing.T) {
	cases := []struct {
		kind realtimeReloadKind
		fail func(*realtimeMatrixFixture)
	}{
		{kind: realtimeReloadActivity, fail: func(f *realtimeMatrixFixture) { f.events.failNextTaskActivity.Store(true) }},
		{kind: realtimeReloadPlanShow, fail: func(f *realtimeMatrixFixture) { f.plans.failNextShow.Store(true) }},
		{kind: realtimeReloadStats, fail: func(f *realtimeMatrixFixture) { f.metrics.failNext.Store(true) }},
		{kind: realtimeReloadLogs, fail: func(f *realtimeMatrixFixture) { f.events.failNextListEvents.Store(true) }},
	}
	for _, tc := range cases {
		t.Run("failed reload no-advance retry "+realtimeKindName(tc.kind), func(t *testing.T) {
			f, model := newRealtimeMatrixFixture(t)
			setRealtimeDomainView(t, f, &model, tc.kind)
			model.commitDataVersion(tc.kind, 1)
			model.commitDataVersion(realtimeReloadBundle, 2)
			f.watermark.version.Store(2)
			tc.fail(f)

			model, msgs := driveRealtimeTickAll(t, model)
			assertReloadKinds(t, msgs, tc.kind)
			assertRealtimeBaseline(t, model, tc.kind, 1)

			model, msgs = driveRealtimeTickAll(t, model)
			assertReloadKinds(t, msgs, tc.kind)
			assertRealtimeBaseline(t, model, tc.kind, 2)
		})
	}

	t.Run("probe error no-advance retry stats", func(t *testing.T) {
		f, model := newRealtimeMatrixFixture(t)
		setRealtimeDomainView(t, f, &model, realtimeReloadStats)
		model.commitDataVersion(realtimeReloadStats, 1)
		f.watermark.version.Store(2)
		f.watermark.failNext.Store(true)

		model, msgs := driveRealtimeTickAll(t, model)
		assertReloadKinds(t, msgs, realtimeReloadStats)
		assertRealtimeBaseline(t, model, realtimeReloadStats, 1)

		model, msgs = driveRealtimeTickAll(t, model)
		assertReloadKinds(t, msgs, realtimeReloadStats)
		assertRealtimeBaseline(t, model, realtimeReloadStats, 2)
	})
}

func TestRealtimeTickPlanNetworkMultiDomainConsumerIndependence(t *testing.T) {
	t.Run("bundle-only bump refreshes bundle while planShow is untouched", func(t *testing.T) {
		f, model := newRealtimeMatrixFixture(t)
		openPlanNetworkFixture(t, f, &model)
		model.commitDataVersion(realtimeReloadBundle, 1)
		model.commitDataVersion(realtimeReloadPlanShow, 2)
		f.watermark.version.Store(2)
		boardTitle := "plan-network-bundle-only"
		if _, err := f.store.CreateTask(f.ctx, f.project.ID, boardTitle, "", domain.Priority(2), "backlog", nil, f.store.Snapshot()); err != nil {
			t.Fatalf("CreateTask(%s) error = %v", boardTitle, err)
		}
		f.tasks.listCalls.Store(0)
		f.plans.getBySlugCalls.Store(0)

		model, msgs := driveRealtimeTickAll(t, model)
		assertReloadKinds(t, msgs, realtimeReloadBundle)
		assertRealtimeBaseline(t, model, realtimeReloadBundle, 2)
		assertRealtimeBaseline(t, model, realtimeReloadPlanShow, 2)
		if got := f.tasks.listCalls.Load(); got == 0 {
			t.Fatal("plan-network bundle-only bump did not refresh bundle-derived task data")
		}
		if got := f.plans.getBySlugCalls.Load(); got != 0 {
			t.Fatalf("planShow reloaded on bundle-only bump: GetPlanBySlug calls = %d, want 0", got)
		}
		if !taskTitlePresent(model.tasks, boardTitle) {
			t.Fatalf("plan-network bundle-only bump missing bundle task %q", boardTitle)
		}
	})

	t.Run("bundle and planShow bumps advance independently", func(t *testing.T) {
		f, model := newRealtimeMatrixFixture(t)
		openPlanNetworkFixture(t, f, &model)
		model.commitDataVersion(realtimeReloadBundle, 2)
		model.commitDataVersion(realtimeReloadPlanShow, 2)
		f.watermark.version.Store(3)
		boardTitle := "plan-network-both-bundle"
		if _, err := f.store.CreateTask(f.ctx, f.project.ID, boardTitle, "", domain.Priority(2), "backlog", nil, f.store.Snapshot()); err != nil {
			t.Fatalf("CreateTask(%s) error = %v", boardTitle, err)
		}
		if _, err := f.store.UpdatePlanGoalBody(f.ctx, f.project.ID, f.plan.ID, "updated goal from planShow bump"); err != nil {
			t.Fatalf("UpdatePlanGoalBody() error = %v", err)
		}
		f.tasks.listCalls.Store(0)
		f.plans.getBySlugCalls.Store(0)

		model, msgs := driveRealtimeTickAll(t, model)
		assertReloadKinds(t, msgs, realtimeReloadBundle, realtimeReloadPlanShow)
		assertRealtimeBaseline(t, model, realtimeReloadBundle, 3)
		assertRealtimeBaseline(t, model, realtimeReloadPlanShow, 3)
		if got := f.tasks.listCalls.Load(); got == 0 {
			t.Fatal("bundle+planShow bump did not refresh bundle-derived task data")
		}
		if got := f.plans.getBySlugCalls.Load(); got == 0 {
			t.Fatal("bundle+planShow bump did not refresh planShow data")
		}
		if !taskTitlePresent(model.tasks, boardTitle) {
			t.Fatalf("bundle+planShow bump missing bundle task %q", boardTitle)
		}
		if model.planNetworkShow.Plan.GoalBody != "updated goal from planShow bump" {
			t.Fatalf("planShow goal = %q, want updated goal", model.planNetworkShow.Plan.GoalBody)
		}
	})
}

func TestRealtimeTickRapidCrossViewWritesDoNotMaskPendingDomain(t *testing.T) {
	f, model := newRealtimeMatrixFixture(t)
	model.commitDataVersion(realtimeReloadBundle, 1)
	model.commitDataVersion(realtimeReloadStats, 1)
	model.commitDataVersion(realtimeReloadLogs, 1)

	setRealtimeDomainView(t, f, &model, realtimeReloadStats)
	firstBoardTitle := "rapid-cross-view-bundle-write"
	if _, err := f.store.CreateTask(f.ctx, f.project.ID, firstBoardTitle, "", domain.Priority(2), "backlog", nil, f.store.Snapshot()); err != nil {
		t.Fatalf("CreateTask(%s) error = %v", firstBoardTitle, err)
	}
	f.watermark.version.Store(2)
	model, msgs := driveRealtimeTickAll(t, model)
	assertReloadKinds(t, msgs, realtimeReloadStats)
	assertRealtimeBaseline(t, model, realtimeReloadStats, 2)
	assertRealtimeBaseline(t, model, realtimeReloadBundle, 1)

	setRealtimeDomainView(t, f, &model, realtimeReloadLogs)
	if err := f.store.RecordEntityEvent(f.ctx, "system", 0, f.project.ID, domain.EventTypeBundleSwapped, `{"matrix":"rapid"}`); err != nil {
		t.Fatalf("RecordEntityEvent() error = %v", err)
	}
	f.watermark.version.Store(3)
	model, msgs = driveRealtimeTickAll(t, model)
	assertReloadKinds(t, msgs, realtimeReloadLogs)
	assertRealtimeBaseline(t, model, realtimeReloadLogs, 3)
	assertRealtimeBaseline(t, model, realtimeReloadBundle, 1)

	setRealtimeDomainView(t, f, &model, realtimeReloadBundle)
	f.tasks.listCalls.Store(0)
	model, msgs = driveRealtimeTickAll(t, model)
	assertReloadKinds(t, msgs, realtimeReloadBundle)
	assertRealtimeBaseline(t, model, realtimeReloadBundle, 3)
	if got := f.tasks.listCalls.Load(); got == 0 {
		t.Fatal("pending bundle domain was masked by later stats/logs writes; ListTasks not called on board return")
	}
	if !taskTitlePresent(model.tasks, firstBoardTitle) {
		t.Fatalf("pending bundle domain did not catch up after rapid cross-view writes; missing %q", firstBoardTitle)
	}
}

// TestRealtimeReloadStaleScopeDroppedAfterNavigation pins audit #65782 (F4):
// the per-domain generation guard only orders async reloads WITHIN a domain; a
// synchronous navigation that swaps the active entity does not bump the gen, so
// a slow worker captured for the previous entity can still pass F3. The
// stale-scope guard must drop that fold (entity-scoped activity by task id,
// plan-show by slug) and must NOT commit the watermark/generation, so the next
// tick reloads the current entity cleanly. A matching-scope fold still applies.
func TestRealtimeReloadStaleScopeDroppedAfterNavigation(t *testing.T) {
	t.Run("activity reload for a different task is dropped", func(t *testing.T) {
		var m Model
		m.taskID = 5 // user is now viewing task #5

		// Stale worker captured task #99 before the user navigated to #5.
		m.applyRealtimeReload(realtimeReloadMsg{
			kind:             realtimeReloadActivity,
			gen:              1,
			scopeTaskID:      99,
			activityForID:    99,
			activity:         []domain.Event{{ID: 1, Body: "stale-task-99"}},
			activityValid:    true,
			dataVersion:      9,
			dataVersionValid: true,
		})
		if len(m.activity) != 0 {
			t.Fatalf("stale-scope activity folded; activity=%#v, want dropped", m.activity)
		}
		if _, ok := m.dataVersionBaseline(realtimeReloadActivity); ok {
			t.Fatal("stale-scope activity drop committed the activity baseline; must leave it uncommitted for retry")
		}

		// A fold scoped to the current task #5 still applies.
		m.applyRealtimeReload(realtimeReloadMsg{
			kind:             realtimeReloadActivity,
			gen:              2,
			scopeTaskID:      5,
			activityForID:    5,
			activity:         []domain.Event{{ID: 2, Body: "match-task-5"}},
			activityValid:    true,
			dataVersion:      9,
			dataVersionValid: true,
		})
		if len(m.activity) != 1 || m.activityForTask != 5 {
			t.Fatalf("matching-scope activity not folded; activity=%#v forTask=%d", m.activity, m.activityForTask)
		}
		assertRealtimeBaseline(t, m, realtimeReloadActivity, 9)
	})

	t.Run("plan-show reload for a different plan is dropped", func(t *testing.T) {
		var m Model
		m.planNetworkShow = app.PlanShow{Plan: domain.Plan{ID: 1, Slug: "alpha"}}

		// Stale worker captured plan "beta" before the user switched to "alpha".
		m.applyRealtimeReload(realtimeReloadMsg{
			kind:             realtimeReloadPlanShow,
			gen:              1,
			scopeSlug:        "beta",
			planShow:         app.PlanShow{Plan: domain.Plan{ID: 2, Slug: "beta", GoalBody: "stale-beta"}},
			planValid:        true,
			dataVersion:      9,
			dataVersionValid: true,
		})
		if m.planNetworkShow.Plan.Slug != "alpha" {
			t.Fatalf("stale-scope planShow clobbered the active plan; slug=%q, want alpha", m.planNetworkShow.Plan.Slug)
		}
		if _, ok := m.dataVersionBaseline(realtimeReloadPlanShow); ok {
			t.Fatal("stale-scope planShow drop committed the planShow baseline; must leave it uncommitted for retry")
		}

		// A fold scoped to the current plan "alpha" still applies.
		m.applyRealtimeReload(realtimeReloadMsg{
			kind:             realtimeReloadPlanShow,
			gen:              2,
			scopeSlug:        "alpha",
			planShow:         app.PlanShow{Plan: domain.Plan{ID: 1, Slug: "alpha", GoalBody: "fresh-alpha"}},
			planValid:        true,
			dataVersion:      9,
			dataVersionValid: true,
		})
		if m.planNetworkShow.Plan.GoalBody != "fresh-alpha" {
			t.Fatalf("matching-scope planShow not folded; goal=%q, want fresh-alpha", m.planNetworkShow.Plan.GoalBody)
		}
		assertRealtimeBaseline(t, m, realtimeReloadPlanShow, 9)
	})
}

func Test1289RealtimeTickInvariantsByName(t *testing.T) {
	t.Run("idle tick = exactly 1 probe / 0 rebuild across N idle ticks", func(t *testing.T) {
		f, model := newRealtimeMatrixFixture(t)
		setRealtimeDomainView(t, f, &model, realtimeReloadBundle)
		model, msgs := driveRealtimeTickAll(t, model)
		assertReloadKinds(t, msgs, realtimeReloadBundle)
		assertRealtimeBaseline(t, model, realtimeReloadBundle, 1)
		f.tasks.listCalls.Store(0)
		probesBefore := f.watermark.calls.Load()

		const idleTicks = 5
		for i := 0; i < idleTicks; i++ {
			model, _ = updateRealtimeTick(t, model)
		}
		if got := f.watermark.calls.Load() - probesBefore; got != idleTicks {
			t.Fatalf("idle ticks ran %d probes, want exactly %d", got, idleTicks)
		}
		if got := f.tasks.listCalls.Load(); got != 0 {
			t.Fatalf("idle ticks rebuilt bundle %d time(s), want 0", got)
		}
	})

	t.Run("off-thread worker never references m", func(t *testing.T) {
		srcBytes, err := os.ReadFile("model.go")
		if err != nil {
			t.Fatalf("ReadFile(model.go) error = %v", err)
		}
		src := string(srcBytes)
		start := strings.Index(src, "func (m *Model) realtimeRefreshCmd")
		if start < 0 {
			t.Fatal("realtimeRefreshCmd source not found")
		}
		endRel := strings.Index(src[start:], "// realtimeReloadRegistry")
		if endRel < 0 {
			t.Fatal("realtimeRefreshCmd end sentinel not found")
		}
		body := src[start : start+endRel]
		parts := strings.Split(body, "cmd = func() tea.Msg {")
		if len(parts) < 2 {
			t.Fatal("realtimeRefreshCmd worker closures not found")
		}
		for i, part := range parts[1:] {
			end := strings.Index(part, "registerRealtimeReloadCmd(cmd)")
			if end < 0 {
				t.Fatalf("worker closure %d end sentinel not found", i+1)
			}
			closure := part[:end]
			if strings.Contains(closure, "m.") {
				t.Fatalf("worker closure %d references m; off-thread reload workers must use captured inputs only:\n%s", i+1, closure)
			}
		}
	})

	t.Run("generation guard drops an older-gen msg", func(t *testing.T) {
		var model Model
		newer := realtimeReloadMsg{
			kind:             realtimeReloadBundle,
			gen:              2,
			dataVersion:      20,
			dataVersionValid: true,
			snap:             app.TUISnapshot{Tasks: []domain.Task{{ID: 1, Title: "newer"}, {ID: 2, Title: "newer-2"}}},
			snapValid:        true,
		}
		older := realtimeReloadMsg{
			kind:             realtimeReloadBundle,
			gen:              1,
			dataVersion:      10,
			dataVersionValid: true,
			snap:             app.TUISnapshot{Tasks: []domain.Task{{ID: 1, Title: "older"}}},
			snapValid:        true,
		}
		model.applyRealtimeReload(newer)
		model.applyRealtimeReload(older)
		if len(model.tasks) != 2 || model.tasks[0].Title != "newer" {
			t.Fatalf("older-gen msg was not dropped; tasks=%#v", model.tasks)
		}
		assertRealtimeBaseline(t, model, realtimeReloadBundle, 20)
	})
}

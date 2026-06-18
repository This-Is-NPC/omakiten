package app

import (
	"context"
	"reflect"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// rollupFakeRepo is an in-memory PlanRepository fixture for ListRollups: it
// serves plans / waves / tasks from maps and counts every read so a test can
// assert the rollup path issues a constant number of queries regardless of
// plan count (the 1+3N -> 3 regression). Only the methods ListRollups and the
// reference per-plan loop touch are implemented; the rest panic so an
// accidental dependency on an un-fixtured method is caught loudly.
type rollupFakeRepo struct {
	plans     []domain.Plan
	wavesByPlan map[int64][]domain.PlanWave
	tasksByPlan map[int64][]domain.PlanTaskRow

	listPlansCalls    int
	bulkWavesCalls    int
	bulkTasksCalls    int
	perPlanWavesCalls int
	perPlanTasksCalls int
	perPlanDepsCalls  int
}

func (r *rollupFakeRepo) ListPlans(_ context.Context, _ int64) ([]domain.Plan, error) {
	r.listPlansCalls++
	return append([]domain.Plan(nil), r.plans...), nil
}

func (r *rollupFakeRepo) ListProjectPlanWaves(_ context.Context, _ int64) ([]domain.PlanWave, error) {
	r.bulkWavesCalls++
	// Flatten in plan order, mirroring the (plan_id, position) ORDER BY.
	var out []domain.PlanWave
	for _, p := range r.plans {
		out = append(out, r.wavesByPlan[p.ID]...)
	}
	return out, nil
}

func (r *rollupFakeRepo) ListProjectPlanTasks(_ context.Context, _ int64, _ domain.BucketResolver) ([]domain.ProjectPlanTaskRow, error) {
	r.bulkTasksCalls++
	var out []domain.ProjectPlanTaskRow
	for _, p := range r.plans {
		for _, t := range r.tasksByPlan[p.ID] {
			out = append(out, domain.ProjectPlanTaskRow{PlanID: p.ID, PlanTaskRow: t})
		}
	}
	return out, nil
}

// Per-plan methods power the reference loop the test uses to compute the
// expected rollups via the legacy 1+3N shape.
func (r *rollupFakeRepo) ListPlanWaves(_ context.Context, _, planID int64) ([]domain.PlanWave, error) {
	r.perPlanWavesCalls++
	return append([]domain.PlanWave(nil), r.wavesByPlan[planID]...), nil
}

func (r *rollupFakeRepo) ListPlanTasks(_ context.Context, _, planID int64, _ domain.BucketResolver) ([]domain.PlanTaskRow, error) {
	r.perPlanTasksCalls++
	return append([]domain.PlanTaskRow(nil), r.tasksByPlan[planID]...), nil
}

func (r *rollupFakeRepo) ListPlanTaskDependencies(_ context.Context, _, _ int64) ([]domain.TaskDependency, error) {
	r.perPlanDepsCalls++
	return nil, nil
}

// Unused PlanRepository surface — panics keep the fixture honest.
func (r *rollupFakeRepo) CreatePlan(context.Context, int64, string, string, string) (domain.Plan, error) {
	panic("unused")
}
func (r *rollupFakeRepo) GetPlanBySlug(context.Context, int64, string) (domain.Plan, error) {
	panic("unused")
}
func (r *rollupFakeRepo) GetPlanByID(context.Context, int64, int64) (domain.Plan, error) {
	panic("unused")
}
func (r *rollupFakeRepo) UpdatePlanGoalBody(context.Context, int64, int64, string) (domain.Plan, error) {
	panic("unused")
}
func (r *rollupFakeRepo) UpdatePlan(context.Context, int64, int64, *string, *string, *string) (domain.Plan, error) {
	panic("unused")
}
func (r *rollupFakeRepo) DeletePlan(context.Context, int64, int64) (domain.Event, error) {
	panic("unused")
}
func (r *rollupFakeRepo) AddPlanWave(context.Context, int64, int64, string, int) (domain.PlanWave, error) {
	panic("unused")
}
func (r *rollupFakeRepo) RemovePlanWave(context.Context, int64, int64) (domain.PlanWave, error) {
	panic("unused")
}
func (r *rollupFakeRepo) RenamePlanWave(context.Context, int64, int64, string) (domain.PlanWave, error) {
	panic("unused")
}
func (r *rollupFakeRepo) ReorderPlanWave(context.Context, int64, int64, int) (domain.PlanWave, error) {
	panic("unused")
}
func (r *rollupFakeRepo) UnassignTaskFromPlan(context.Context, int64, int64) (domain.Event, error) {
	panic("unused")
}
func (r *rollupFakeRepo) AssignTaskToPlan(context.Context, int64, int64, int64, int64) error {
	panic("unused")
}
func (r *rollupFakeRepo) ClaimNextPlanTask(context.Context, int64, int64, domain.BucketResolver) (domain.Task, bool, error) {
	panic("unused")
}
func (r *rollupFakeRepo) PeekNextClaimable(context.Context, int64, int64, domain.BucketResolver) (domain.PlanTaskRow, bool, error) {
	panic("unused")
}
func (r *rollupFakeRepo) MaybeFinalizePlanForTask(context.Context, int64, int64, domain.BucketResolver) (bool, error) {
	panic("unused")
}

// rollupSnapshot builds a snapshot whose workflow final bucket is "done" so
// DoneCount has something to resolve against.
func rollupSnapshot() *config.Snapshot {
	return config.BuildSnapshot(config.Bundle{
		Workflows: []config.Workflow{
			{
				ID:   1,
				Key:  "wf",
				Name: "WF",
				Buckets: []config.Bucket{
					{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
					{ID: 2, Key: "dev", Name: "Dev", Position: 2},
					{ID: 3, Key: "done", Name: "Done", Position: 3},
				},
			},
		},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "wf"}},
	})
}

func rollupFixture(planCount int) *rollupFakeRepo {
	repo := &rollupFakeRepo{
		wavesByPlan: map[int64][]domain.PlanWave{},
		tasksByPlan: map[int64][]domain.PlanTaskRow{},
	}
	for i := 1; i <= planCount; i++ {
		pid := int64(i)
		repo.plans = append(repo.plans, domain.Plan{ID: pid, ProjectID: 7, Slug: "p", Name: "P"})
		w1 := domain.PlanWave{ID: pid*10 + 1, PlanID: pid, Name: "Wave 1", Position: 1}
		w2 := domain.PlanWave{ID: pid*10 + 2, PlanID: pid, Name: "Wave 2", Position: 2}
		repo.wavesByPlan[pid] = []domain.PlanWave{w1, w2}
		repo.tasksByPlan[pid] = []domain.PlanTaskRow{
			// Wave 1: one done, one in dev -> wave incomplete -> active wave.
			{TaskID: pid*100 + 1, WaveID: w1.ID, Title: "t1", BucketKey: "done", State: domain.TaskStateActive},
			{TaskID: pid*100 + 2, WaveID: w1.ID, Title: "t2", BucketKey: "dev", State: domain.TaskStateActive},
			// archived task is excluded from totals.
			{TaskID: pid*100 + 3, WaveID: w1.ID, Title: "t3", BucketKey: "dev", State: domain.TaskStateArchived},
			// Wave 2: one in backlog.
			{TaskID: pid*100 + 4, WaveID: w2.ID, Title: "t4", BucketKey: "backlog", State: domain.TaskStateActive},
		}
	}
	return repo
}

// referenceRollups recomputes the rollups via the legacy per-plan 1+3N loop so
// the bulk path's output can be compared against it (and the per-plan call
// counts recorded for the regression assertion).
func referenceRollups(t *testing.T, repo *rollupFakeRepo, snap *config.Snapshot) []PlanRollup {
	t.Helper()
	ctx := context.Background()
	project := domain.ProjectContext{ID: 7}
	final := snap.Workflow().FinalBucketKey()
	plans, _ := repo.ListPlans(ctx, project.ID)
	out := make([]PlanRollup, 0, len(plans))
	for _, p := range plans {
		waves, _ := repo.ListPlanWaves(ctx, project.ID, p.ID)
		tasks, _ := repo.ListPlanTasks(ctx, project.ID, p.ID, snap)
		_, _ = repo.ListPlanTaskDependencies(ctx, project.ID, p.ID)
		out = append(out, foldPlanRollup(p, waves, tasks, final))
	}
	return out
}

func TestListRollupsBulkHydrationQueryCountConstant(t *testing.T) {
	snap := rollupSnapshot()
	ctx := context.Background()
	project := domain.ProjectContext{ID: 7}

	for _, planCount := range []int{1, 5, 20} {
		repo := rollupFixture(planCount)
		svc := NewPlanServiceWithSnapshot(repo, snap)
		if _, err := svc.ListRollups(ctx, project); err != nil {
			t.Fatalf("planCount=%d ListRollups: %v", planCount, err)
		}
		// Bulk path: exactly 1 ListPlans + 1 bulk waves + 1 bulk tasks = 3
		// queries, independent of planCount. The legacy shape was 1+3N.
		if repo.listPlansCalls != 1 || repo.bulkWavesCalls != 1 || repo.bulkTasksCalls != 1 {
			t.Fatalf("planCount=%d query counts: plans=%d bulkWaves=%d bulkTasks=%d, want 1/1/1",
				planCount, repo.listPlansCalls, repo.bulkWavesCalls, repo.bulkTasksCalls)
		}
		if repo.perPlanWavesCalls != 0 || repo.perPlanTasksCalls != 0 || repo.perPlanDepsCalls != 0 {
			t.Fatalf("planCount=%d issued per-plan N+1 queries: waves=%d tasks=%d deps=%d, want 0/0/0",
				planCount, repo.perPlanWavesCalls, repo.perPlanTasksCalls, repo.perPlanDepsCalls)
		}
	}
}

func TestListRollupsBulkHydrationOutputMatchesPerPlan(t *testing.T) {
	snap := rollupSnapshot()
	ctx := context.Background()
	project := domain.ProjectContext{ID: 7}

	repo := rollupFixture(6)
	svc := NewPlanServiceWithSnapshot(repo, snap)
	got, err := svc.ListRollups(ctx, project)
	if err != nil {
		t.Fatalf("ListRollups: %v", err)
	}

	// Fresh repo for the reference loop so its call counters do not mix with
	// the bulk run above.
	want := referenceRollups(t, rollupFixture(6), snap)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bulk rollups differ from per-plan reference\n got=%+v\nwant=%+v", got, want)
	}
	// Sanity-check the fixture actually exercised the interesting fields.
	for _, r := range got {
		if r.TotalCount != 3 || r.DoneCount != 1 {
			t.Fatalf("rollup counts = done %d/total %d, want 1/3", r.DoneCount, r.TotalCount)
		}
		if r.ActiveWaveID == 0 || r.ActiveWaveName != "Wave 1" {
			t.Fatalf("active wave = id %d name %q, want first incomplete wave 'Wave 1'", r.ActiveWaveID, r.ActiveWaveName)
		}
	}
}

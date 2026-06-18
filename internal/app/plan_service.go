package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// PlanService wraps the plan persistence layer with input normalisation,
// activity tracking, and (eventually) plan-status transitions. v1 ships
// the create + list + show + add-wave primitives; assign-task and
// claim-next land in subsequent slices.
type PlanService struct {
	repo PlanRepository
	snap *config.Snapshot
}

func NewPlanService(repo PlanRepository) *PlanService {
	return &PlanService{repo: repo}
}

// NewPlanServiceWithSnapshot captures the per-project Snapshot the Show
// path needs to resolve the workflow's final bucket key when computing
// done counts. The Create/List paths do not consult it.
func NewPlanServiceWithSnapshot(repo PlanRepository, snap *config.Snapshot) *PlanService {
	return &PlanService{repo: repo, snap: snap}
}

// Create normalises the slug / name pair and delegates to the repo. The
// repo emits plan.created in the same transaction as the insert; this
// wrapper exists so the agent layer (which composes against the app port)
// can grow business rules without touching the sqlite adapter.
func (s *PlanService) Create(ctx context.Context, project domain.ProjectContext, slug, name, goalBody string) (plan domain.Plan, err error) {
	finish := activity.Track(ctx, "app.PlanService.Create", project, map[string]any{"slug": slug})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	if err = domain.ValidatePlanGoalBody(goalBody); err != nil {
		return
	}
	plan, err = s.repo.CreatePlan(ctx, project.ID, slug, name, goalBody)
	return
}

// List returns every plan for the project, ordered by id ascending.
func (s *PlanService) List(ctx context.Context, project domain.ProjectContext) (plans []domain.Plan, err error) {
	finish := activity.Track(ctx, "app.PlanService.List", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	plans, err = s.repo.ListPlans(ctx, project.ID)
	return
}

// GetBySlug resolves a plan by its slug, returning ErrPlanNotFound when
// the slug does not exist in the active project.
func (s *PlanService) GetBySlug(ctx context.Context, project domain.ProjectContext, slug string) (domain.Plan, error) {
	return s.repo.GetPlanBySlug(ctx, project.ID, strings.TrimSpace(slug))
}

// AssignTask attaches an existing task to a (plan, wave). The repo
// rejects cross-project / cross-plan mismatches via ErrPlanNotFound /
// ErrPlanWaveNotFound; no events emit on plan/wave linkage (per slice 3
// design — wave membership shows up indirectly through task.moved when
// the task transitions).
func (s *PlanService) AssignTask(ctx context.Context, project domain.ProjectContext, taskID, planID, waveID int64) (err error) {
	finish := activity.Track(ctx, "app.PlanService.AssignTask", project, map[string]any{"task_id": taskID, "plan_id": planID, "wave_id": waveID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	return s.repo.AssignTaskToPlan(ctx, project.ID, taskID, planID, waveID)
}

// ClaimNext is the atomic-claim primitive: serialised behind SQLite's
// reserved write lock, picks the next claimable task in the plan's
// active wave, and marks it claimed by the caller's _agent_model.
// Claimable means active, unassigned, and still in the workflow's first
// bucket; bucket movement remains a separate WorkflowService move.
// Returns (task, true) on a successful claim, (zero, false) when no
// task is claimable. The Snapshot captured by NewPlanServiceWithSnapshot
// supplies the BucketResolver.
func (s *PlanService) ClaimNext(ctx context.Context, project domain.ProjectContext, planID int64) (task domain.Task, claimed bool, err error) {
	finish := activity.Track(ctx, "app.PlanService.ClaimNext", project, map[string]any{"plan_id": planID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	if s.snap == nil {
		return domain.Task{}, false, domain.NewError(domain.ErrConfigInvalid,
			"plans.claim_next requires a snapshot-bound PlanService", nil)
	}
	return s.repo.ClaimNextPlanTask(ctx, project.ID, planID, s.snap)
}

// UpdateGoalBody rewrites the plan's goal_body column. The repo emits
// plan.goal_edited so downstream surfaces (FTS5 search, metrics) see
// the edit on the next refresh.
func (s *PlanService) UpdateGoalBody(ctx context.Context, project domain.ProjectContext, planID int64, goalBody string) (plan domain.Plan, err error) {
	finish := activity.Track(ctx, "app.PlanService.UpdateGoalBody", project, map[string]any{"plan_id": planID, "length": len(goalBody)})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	if err = domain.ValidatePlanGoalBody(goalBody); err != nil {
		return
	}
	plan, err = s.repo.UpdatePlanGoalBody(ctx, project.ID, planID, goalBody)
	return
}

// UpdatePlan mutates a plan's name / slug / status. Each field is
// optional (nil pointer = leave untouched). The repo emits plan.edited
// (and plan.abandoned when status→abandoned). Slug collisions surface as
// ErrPlanSlugConflict; a no-op set surfaces as ErrValidation.
func (s *PlanService) UpdatePlan(ctx context.Context, project domain.ProjectContext, planID int64, name, slug, status *string) (plan domain.Plan, err error) {
	finish := activity.Track(ctx, "app.PlanService.UpdatePlan", project, map[string]any{"plan_id": planID})
	defer func() {
		st := "ok"
		errMsg := ""
		if err != nil {
			st = "error"
			errMsg = err.Error()
		}
		finish(st, errMsg)
	}()
	plan, err = s.repo.UpdatePlan(ctx, project.ID, planID, name, slug, status)
	return
}

// DeletePlan hard-deletes a plan. Waves cascade; member tasks survive
// with plan_id / wave_id cleared (FK SET NULL). The repo emits
// plan.deleted; the returned event is the deletion record.
func (s *PlanService) DeletePlan(ctx context.Context, project domain.ProjectContext, planID int64) (event domain.Event, err error) {
	finish := activity.Track(ctx, "app.PlanService.DeletePlan", project, map[string]any{"plan_id": planID})
	defer func() {
		st := "ok"
		errMsg := ""
		if err != nil {
			st = "error"
			errMsg = err.Error()
		}
		finish(st, errMsg)
	}()
	event, err = s.repo.DeletePlan(ctx, project.ID, planID)
	return
}

// PeekNextClaimable returns the next task plans.claim_next would
// reserve, without mutating anything. Snapshot-bound: requires the
// same BucketResolver ClaimNext depends on.
func (s *PlanService) PeekNextClaimable(ctx context.Context, project domain.ProjectContext, planID int64) (row domain.PlanTaskRow, ok bool, err error) {
	finish := activity.Track(ctx, "app.PlanService.PeekNextClaimable", project, map[string]any{"plan_id": planID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	if s.snap == nil {
		return domain.PlanTaskRow{}, false, domain.NewError(domain.ErrConfigInvalid,
			"plans.peek_next_claimable requires a snapshot-bound PlanService", nil)
	}
	return s.repo.PeekNextClaimable(ctx, project.ID, planID, s.snap)
}

// AddWave appends a wave to a plan. Position 0 (or negative) auto-assigns
// after the current highest position. The repo emits plan.wave_added.
func (s *PlanService) AddWave(ctx context.Context, project domain.ProjectContext, planID int64, name string, position int) (wave domain.PlanWave, err error) {
	finish := activity.Track(ctx, "app.PlanService.AddWave", project, map[string]any{"plan_id": planID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	wave, err = s.repo.AddPlanWave(ctx, project.ID, planID, strings.TrimSpace(name), position)
	return
}

// RemoveWave deletes a wave from a plan. Member tasks survive with their
// wave_id cleared (FK SET NULL); plan_id is untouched. The repo emits
// plan.wave_removed. Returns ErrPlanWaveNotFound when the wave does not
// belong to the project.
func (s *PlanService) RemoveWave(ctx context.Context, project domain.ProjectContext, waveID int64) (wave domain.PlanWave, err error) {
	finish := activity.Track(ctx, "app.PlanService.RemoveWave", project, map[string]any{"wave_id": waveID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	wave, err = s.repo.RemovePlanWave(ctx, project.ID, waveID)
	return
}

// RenameWave rewrites a wave's name. The repo emits plan.wave_renamed.
// A blank or no-op name surfaces as ErrValidation; an unknown/out-of-
// project wave surfaces as ErrPlanWaveNotFound.
func (s *PlanService) RenameWave(ctx context.Context, project domain.ProjectContext, waveID int64, name string) (wave domain.PlanWave, err error) {
	finish := activity.Track(ctx, "app.PlanService.RenameWave", project, map[string]any{"wave_id": waveID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	wave, err = s.repo.RenamePlanWave(ctx, project.ID, waveID, strings.TrimSpace(name))
	return
}

// ReorderWave moves a wave to newPosition (1-based) within its plan,
// swapping with the slot's occupant on collision. The repo emits
// plan.wave_reordered. newPosition <= 0 and no-op moves surface as
// ErrValidation; an unknown/out-of-project wave surfaces as
// ErrPlanWaveNotFound.
func (s *PlanService) ReorderWave(ctx context.Context, project domain.ProjectContext, waveID int64, newPosition int) (wave domain.PlanWave, err error) {
	finish := activity.Track(ctx, "app.PlanService.ReorderWave", project, map[string]any{"wave_id": waveID, "position": newPosition})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	wave, err = s.repo.ReorderPlanWave(ctx, project.ID, waveID, newPosition)
	return
}

// UnassignTask detaches a task from its plan, clearing both plan_id and
// wave_id (full detach). The repo emits plan.task_unassigned (no event
// when the task was already detached). Returns ErrTaskNotFound when the
// task does not belong to the project.
func (s *PlanService) UnassignTask(ctx context.Context, project domain.ProjectContext, taskID int64) (event domain.Event, err error) {
	finish := activity.Track(ctx, "app.PlanService.UnassignTask", project, map[string]any{"task_id": taskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	event, err = s.repo.UnassignTaskFromPlan(ctx, project.ID, taskID)
	return
}

// PlanShow is the aggregated view PlanService.Show returns. The active
// wave is the lowest-position wave whose tasks are not all in the
// workflow's final bucket; ActiveWaveID is 0 when every wave is done
// (or when the plan has no waves yet). Dependencies enumerates the
// in-plan task→task edges (both endpoints belong to this plan) so the
// network renderer can draw blocker markers without a follow-up query.
type PlanShow struct {
	Plan         domain.Plan             `json:"plan"`
	Waves        []PlanWaveView          `json:"waves"`
	DoneCount    int                     `json:"done_count"`
	TotalCount   int                     `json:"total_count"`
	ActiveWaveID int64                   `json:"active_wave_id,omitempty"`
	Dependencies []domain.TaskDependency `json:"dependencies,omitempty"`
}

// PlanWaveView pairs a wave with its tasks and per-wave done/total
// counts. Used by the TUI network diagram and by MCP plans.show.
type PlanWaveView struct {
	Wave       domain.PlanWave      `json:"wave"`
	Tasks      []domain.PlanTaskRow `json:"tasks,omitempty"`
	DoneCount  int                  `json:"done_count"`
	TotalCount int                  `json:"total_count"`
}

// PlanRollup is the lightweight per-plan projection the TUI list view
// consumes — slug/name/status from domain.Plan plus the aggregated
// done/total counters and the active wave's display name. Waves and
// per-task detail stay out of this projection so callers do not pay
// the per-task scan cost for a one-line row.
type PlanRollup struct {
	Plan           domain.Plan `json:"plan"`
	DoneCount      int         `json:"done_count"`
	TotalCount     int         `json:"total_count"`
	ActiveWaveID   int64       `json:"active_wave_id,omitempty"`
	ActiveWaveName string      `json:"active_wave_name,omitempty"`
}

// Show resolves a plan by slug and folds its waves + tasks into a single
// projection ready for MCP / TUI rendering. ErrPlanNotFound bubbles when
// the slug is missing in the active project. Archived tasks are filtered
// out of the counts but stay in the wave's Tasks list so the renderer
// can decide whether to render them — keeps the percentage formula
// honest ("done out of active") while preserving the audit trail.
func (s *PlanService) Show(ctx context.Context, project domain.ProjectContext, slug string) (show PlanShow, err error) {
	finish := activity.Track(ctx, "app.PlanService.Show", project, map[string]any{"slug": slug})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	plan, err := s.repo.GetPlanBySlug(ctx, project.ID, strings.TrimSpace(slug))
	if err != nil {
		return PlanShow{}, err
	}
	return s.composeShow(ctx, project, plan)
}

// composeShow folds a resolved plan with its waves and tasks into the
// aggregated PlanShow projection. Extracted so List-style callers
// (PlanService.ListRollups) can reuse the wave-aggregation logic without
// the slug round-trip Show pays.
func (s *PlanService) composeShow(ctx context.Context, project domain.ProjectContext, plan domain.Plan) (PlanShow, error) {
	waves, err := s.repo.ListPlanWaves(ctx, project.ID, plan.ID)
	if err != nil {
		return PlanShow{}, err
	}
	tasks, err := s.repo.ListPlanTasks(ctx, project.ID, plan.ID, s.snap)
	if err != nil {
		return PlanShow{}, err
	}

	final := s.finalBucketKey()

	tasksByWave := map[int64][]domain.PlanTaskRow{}
	for _, t := range tasks {
		tasksByWave[t.WaveID] = append(tasksByWave[t.WaveID], t)
	}

	show := PlanShow{Plan: plan}
	views := make([]PlanWaveView, 0, len(waves))
	for _, w := range waves {
		view := PlanWaveView{Wave: w, Tasks: tasksByWave[w.ID]}
		view.DoneCount, view.TotalCount = countWaveTasks(view.Tasks, final)
		views = append(views, view)
		show.TotalCount += view.TotalCount
		show.DoneCount += view.DoneCount
	}

	for _, v := range views {
		if v.TotalCount == 0 || v.DoneCount < v.TotalCount {
			show.ActiveWaveID = v.Wave.ID
			break
		}
	}

	show.Waves = views

	deps, err := s.repo.ListPlanTaskDependencies(ctx, project.ID, plan.ID)
	if err != nil {
		return PlanShow{}, err
	}
	show.Dependencies = deps

	return show, nil
}

// ListRollups returns one PlanRollup per plan in the project — the
// lightweight projection the TUI list view consumes. Internally folds
// the same wave-aggregation logic as Show so done/total counts and the
// active-wave selection agree across surfaces. Requires a snapshot-bound
// PlanService (same constraint as Show): without one, the final-bucket
// resolver is empty and DoneCount is always 0.
func (s *PlanService) ListRollups(ctx context.Context, project domain.ProjectContext) (rollups []PlanRollup, err error) {
	finish := activity.Track(ctx, "app.PlanService.ListRollups", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	// Bulk hydration: three project-wide queries (plans + all waves + all
	// tasks) folded in Go, replacing the former 1+3N loop of composeShow
	// (which paid ListPlanWaves + ListPlanTasks + ListPlanTaskDependencies
	// per plan). The dependency query composeShow runs is not needed here —
	// PlanRollup carries no edges — so the rollup path drops to a constant
	// three queries regardless of plan count. The per-plan fold reuses the
	// same wave-aggregation rule as composeShow so output is byte-identical.
	plans, err := s.repo.ListPlans(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	wavesByPlan := map[int64][]domain.PlanWave{}
	allWaves, err := s.repo.ListProjectPlanWaves(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	for _, w := range allWaves {
		wavesByPlan[w.PlanID] = append(wavesByPlan[w.PlanID], w)
	}
	tasksByPlan := map[int64][]domain.PlanTaskRow{}
	allTasks, err := s.repo.ListProjectPlanTasks(ctx, project.ID, s.snap)
	if err != nil {
		return nil, err
	}
	for _, t := range allTasks {
		tasksByPlan[t.PlanID] = append(tasksByPlan[t.PlanID], t.PlanTaskRow)
	}

	rollups = make([]PlanRollup, 0, len(plans))
	for _, p := range plans {
		rollups = append(rollups, foldPlanRollup(p, wavesByPlan[p.ID], tasksByPlan[p.ID], s.finalBucketKey()))
	}
	return rollups, nil
}

// finalBucketKey resolves the workflow's final bucket key from the bound
// snapshot, or "" when no snapshot is attached (DoneCount then stays 0, the
// same degenerate contract Show documents).
func (s *PlanService) finalBucketKey() string {
	if s.snap == nil {
		return ""
	}
	return s.snap.Workflow().FinalBucketKey()
}

// countWaveTasks returns (done, total) for one wave's tasks: archived tasks
// are excluded from both, and a task counts toward done when its bucket is the
// workflow's final bucket. finalBucketKey == "" (no bound snapshot) means done
// is always 0. Shared by composeShow and foldPlanRollup so the Show and
// ListRollups surfaces agree on the counts.
func countWaveTasks(tasks []domain.PlanTaskRow, finalBucketKey string) (done, total int) {
	for _, t := range tasks {
		if t.State == domain.TaskStateArchived {
			continue
		}
		total++
		if finalBucketKey != "" && t.BucketKey == finalBucketKey {
			done++
		}
	}
	return done, total
}

// foldPlanRollup aggregates one plan's waves + tasks into a PlanRollup using
// the identical done/total counting and active-wave selection rule as
// composeShow, so ListRollups output matches the per-plan Show path exactly.
// Extracted so both the bulk rollup fold and composeShow share the algorithm
// rather than copy it.
func foldPlanRollup(plan domain.Plan, waves []domain.PlanWave, tasks []domain.PlanTaskRow, finalBucketKey string) PlanRollup {
	tasksByWave := map[int64][]domain.PlanTaskRow{}
	for _, t := range tasks {
		tasksByWave[t.WaveID] = append(tasksByWave[t.WaveID], t)
	}

	rollup := PlanRollup{Plan: plan}
	type waveAgg struct {
		id         int64
		name       string
		doneCount  int
		totalCount int
	}
	aggs := make([]waveAgg, 0, len(waves))
	for _, w := range waves {
		agg := waveAgg{id: w.ID, name: w.Name}
		agg.doneCount, agg.totalCount = countWaveTasks(tasksByWave[w.ID], finalBucketKey)
		aggs = append(aggs, agg)
		rollup.TotalCount += agg.totalCount
		rollup.DoneCount += agg.doneCount
	}

	for _, a := range aggs {
		if a.totalCount == 0 || a.doneCount < a.totalCount {
			rollup.ActiveWaveID = a.id
			rollup.ActiveWaveName = a.name
			break
		}
	}
	return rollup
}

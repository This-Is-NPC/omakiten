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
// reserved write lock, picks the next unblocked task in the plan's
// active wave and marks it claimed by the caller's _agent_model.
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
	plan, err = s.repo.UpdatePlanGoalBody(ctx, project.ID, planID, goalBody)
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

// PlanShow is the aggregated view PlanService.Show returns. The active
// wave is the lowest-position wave whose tasks are not all in the
// workflow's final bucket; ActiveWaveID is 0 when every wave is done
// (or when the plan has no waves yet). Dependencies enumerates the
// in-plan task→task edges (both endpoints belong to this plan) so the
// network renderer can draw blocker markers without a follow-up query.
type PlanShow struct {
	Plan         domain.Plan              `json:"plan"`
	Waves        []PlanWaveView           `json:"waves"`
	DoneCount    int                      `json:"done_count"`
	TotalCount   int                      `json:"total_count"`
	ActiveWaveID int64                    `json:"active_wave_id,omitempty"`
	Dependencies []domain.TaskDependency  `json:"dependencies,omitempty"`
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

	final := ""
	if s.snap != nil {
		final = s.snap.Workflow().FinalBucketKey()
	}

	tasksByWave := map[int64][]domain.PlanTaskRow{}
	for _, t := range tasks {
		tasksByWave[t.WaveID] = append(tasksByWave[t.WaveID], t)
	}

	show := PlanShow{Plan: plan}
	views := make([]PlanWaveView, 0, len(waves))
	for _, w := range waves {
		view := PlanWaveView{Wave: w, Tasks: tasksByWave[w.ID]}
		for _, t := range view.Tasks {
			if t.State == domain.TaskStateArchived {
				continue
			}
			view.TotalCount++
			if final != "" && t.BucketKey == final {
				view.DoneCount++
			}
		}
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

	if deps, depErr := s.repo.ListPlanTaskDependencies(ctx, project.ID, plan.ID); depErr == nil {
		show.Dependencies = deps
	}

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

	plans, err := s.repo.ListPlans(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	rollups = make([]PlanRollup, 0, len(plans))
	for _, p := range plans {
		show, err := s.composeShow(ctx, project, p)
		if err != nil {
			return nil, err
		}
		rollup := PlanRollup{
			Plan:         show.Plan,
			DoneCount:    show.DoneCount,
			TotalCount:   show.TotalCount,
			ActiveWaveID: show.ActiveWaveID,
		}
		if rollup.ActiveWaveID != 0 {
			for _, v := range show.Waves {
				if v.Wave.ID == rollup.ActiveWaveID {
					rollup.ActiveWaveName = v.Wave.Name
					break
				}
			}
		}
		rollups = append(rollups, rollup)
	}
	return rollups, nil
}

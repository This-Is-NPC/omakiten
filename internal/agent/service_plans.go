package agent

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// newPlanService composes the per-project app.PlanService. Mirrors the
// other newXService helpers; the agent layer never holds a long-lived
// reference because the underlying repo is the only state. The snapshot
// is threaded so PlanService.Show can resolve the workflow's final
// bucket without round-tripping through the repo.
func (s *Service) newPlanService() *app.PlanService {
	return app.NewPlanServiceWithSnapshot(s.repo, s.snapshot)
}

// CreatePlan creates a plan in the resolved project, returning the wire
// projection. Validation lives in app.PlanService → sqlite.Store; this
// layer only resolves the project and projects the response.
func (s *Service) CreatePlan(ctx context.Context, input CreatePlanInput) (CreatePlanResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CreatePlanResponse{}, err
	}
	plan, err := s.newPlanService().Create(ctx, project, input.Slug, input.Name, input.GoalBody)
	if err != nil {
		return CreatePlanResponse{}, err
	}
	return CreatePlanResponse{
		Project: projectSummary(project),
		Plan:    planSummary(plan),
	}, nil
}

// ListPlans returns every plan in the resolved project, oldest first.
// GoalBody is stripped from each entry to keep payloads compact — the
// goal body is full markdown and unbounded by design.
func (s *Service) ListPlans(ctx context.Context, input ListPlansInput) (ListPlansResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ListPlansResponse{}, err
	}
	plans, err := s.newPlanService().List(ctx, project)
	if err != nil {
		return ListPlansResponse{}, err
	}
	summaries := make([]PlanSummary, 0, len(plans))
	for _, p := range plans {
		s := planSummary(p)
		s.GoalBody = ""
		summaries = append(summaries, s)
	}
	return ListPlansResponse{
		Project: projectSummary(project),
		Plans:   summaries,
	}, nil
}

// ShowPlan returns the aggregated plan view: full plan row, waves with
// their task lists, and done/total counts (per wave and overall) plus
// the integer percent (done*100/total, clamped to 0 when total is 0).
// The active wave id is the lowest-position wave that still has pending
// tasks; 0 when every wave is fully done.
func (s *Service) ShowPlan(ctx context.Context, input ShowPlanInput) (ShowPlanResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ShowPlanResponse{}, err
	}
	show, err := s.newPlanService().Show(ctx, project, input.Slug)
	if err != nil {
		return ShowPlanResponse{}, err
	}

	waves := make([]PlanWaveView, 0, len(show.Waves))
	for _, w := range show.Waves {
		view := PlanWaveView{
			ID:         w.Wave.ID,
			Name:       w.Wave.Name,
			Position:   w.Wave.Position,
			DoneCount:  w.DoneCount,
			TotalCount: w.TotalCount,
		}
		for _, t := range w.Tasks {
			row := PlanTaskRow{
				TaskID:     t.TaskID,
				Title:      t.Title,
				BucketKey:  t.BucketKey,
				AssignedTo: t.AssignedTo,
			}
			if t.State != "" && t.State != "active" {
				row.State = string(t.State)
			}
			view.Tasks = append(view.Tasks, row)
		}
		waves = append(waves, view)
	}

	percent := 0
	if show.TotalCount > 0 {
		percent = show.DoneCount * 100 / show.TotalCount
	}

	return ShowPlanResponse{
		Project:      projectSummary(project),
		Plan:         planSummary(show.Plan),
		Waves:        waves,
		DoneCount:    show.DoneCount,
		TotalCount:   show.TotalCount,
		Percent:      percent,
		ActiveWaveID: show.ActiveWaveID,
	}, nil
}

// AssignPlanTask attaches an existing task to a plan + wave. The plan
// is identified by slug or plan_id (slug wins when both supplied);
// wave_id is taken verbatim — supplying a wave from a different plan
// fails with ErrPlanWaveNotFound.
func (s *Service) AssignPlanTask(ctx context.Context, input AssignPlanTaskInput) (AssignPlanTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return AssignPlanTaskResponse{}, err
	}
	planID := input.PlanID
	if input.Slug != "" {
		plan, err := s.newPlanService().GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return AssignPlanTaskResponse{}, err
		}
		planID = plan.ID
	}
	if planID == 0 {
		return AssignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	if input.WaveID == 0 {
		return AssignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	if input.TaskID == 0 {
		return AssignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "task_id is required", nil)
	}
	if err := s.newPlanService().AssignTask(ctx, project, input.TaskID, planID, input.WaveID); err != nil {
		return AssignPlanTaskResponse{}, err
	}
	return AssignPlanTaskResponse{
		Project: projectSummary(project),
		TaskID:  input.TaskID,
		PlanID:  planID,
		WaveID:  input.WaveID,
	}, nil
}

// ClaimNextPlanTask runs the atomic claim primitive. Returns Claimed=false
// (and no Task) when nothing is claimable in the plan's active wave.
func (s *Service) ClaimNextPlanTask(ctx context.Context, input ClaimNextPlanTaskInput) (ClaimNextPlanTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ClaimNextPlanTaskResponse{}, err
	}
	planID := input.PlanID
	if input.Slug != "" {
		plan, err := s.newPlanService().GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return ClaimNextPlanTaskResponse{}, err
		}
		planID = plan.ID
	}
	if planID == 0 {
		return ClaimNextPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	task, claimed, err := s.newPlanService().ClaimNext(ctx, project, planID)
	if err != nil {
		return ClaimNextPlanTaskResponse{}, err
	}
	resp := ClaimNextPlanTaskResponse{Project: projectSummary(project), Claimed: claimed}
	if claimed {
		summary := taskSummary(task, s.registry)
		resp.Task = &summary
	}
	return resp, nil
}

// AddPlanWave appends (position=0) or inserts (position>0) a wave onto
// a plan. Slug is the user-facing plan handle; plan_id may be supplied
// instead when the caller already holds it from a previous response.
func (s *Service) AddPlanWave(ctx context.Context, input AddPlanWaveInput) (AddPlanWaveResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return AddPlanWaveResponse{}, err
	}
	planID := input.PlanID
	if planID == 0 {
		plan, err := s.newPlanService().GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return AddPlanWaveResponse{}, err
		}
		planID = plan.ID
	}
	wave, err := s.newPlanService().AddWave(ctx, project, planID, input.Name, input.Position)
	if err != nil {
		return AddPlanWaveResponse{}, err
	}
	return AddPlanWaveResponse{
		Project: projectSummary(project),
		Wave: PlanWaveSummary{
			ID:       wave.ID,
			PlanID:   wave.PlanID,
			Name:     wave.Name,
			Position: wave.Position,
		},
	}, nil
}

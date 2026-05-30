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

// ContinuePlan returns the agent-tailored projection: ShowPlan's
// aggregate plus a non-mutating preview of the next claimable task.
// Agents picking up work call this before plans.claim_next so they can
// inspect the goal_body and the candidate task without committing to
// the claim.
func (s *Service) ContinuePlan(ctx context.Context, input ContinuePlanInput) (ContinuePlanResponse, error) {
	show, err := s.ShowPlan(ctx, ShowPlanInput(input))
	if err != nil {
		return ContinuePlanResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ContinuePlanResponse{}, err
	}
	resp := ContinuePlanResponse{
		Project:      show.Project,
		Plan:         show.Plan,
		Waves:        show.Waves,
		DoneCount:    show.DoneCount,
		TotalCount:   show.TotalCount,
		Percent:      show.Percent,
		ActiveWaveID: show.ActiveWaveID,
	}
	row, ok, err := s.newPlanService().PeekNextClaimable(ctx, project, show.Plan.ID)
	if err != nil {
		return ContinuePlanResponse{}, err
	}
	if ok {
		preview := PlanTaskRow{
			TaskID:     row.TaskID,
			Title:      row.Title,
			BucketKey:  row.BucketKey,
			AssignedTo: row.AssignedTo,
		}
		if row.State != "" && row.State != "active" {
			preview.State = string(row.State)
		}
		resp.NextClaimable = &preview
	}
	return resp, nil
}

// EditPlan mutates a plan's name / slug / status and/or goal_body. The
// plan is identified by slug or plan_id (slug wins). goal_body edits go
// through UpdateGoalBody (emits plan.goal_edited); name/slug/status edits
// go through UpdatePlan (emits plan.edited, plus plan.abandoned on an
// abandon). At least one editable field must be supplied. Returns the
// post-edit plan projection.
func (s *Service) EditPlan(ctx context.Context, input EditPlanInput) (EditPlanResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return EditPlanResponse{}, err
	}
	svc := s.newPlanService()
	planID := input.PlanID
	if input.Slug != "" {
		plan, err := svc.GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return EditPlanResponse{}, err
		}
		planID = plan.ID
	}
	if planID == 0 {
		return EditPlanResponse{}, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	if input.Name == nil && input.NewSlug == nil && input.Status == nil && input.GoalBody == nil {
		return EditPlanResponse{}, domain.NewError(domain.ErrValidation,
			"plans.edit requires at least one of name, new_slug, status, goal_body", nil)
	}

	var plan domain.Plan
	if input.GoalBody != nil {
		plan, err = svc.UpdateGoalBody(ctx, project, planID, *input.GoalBody)
		if err != nil {
			return EditPlanResponse{}, err
		}
	}
	if input.Name != nil || input.NewSlug != nil || input.Status != nil {
		plan, err = svc.UpdatePlan(ctx, project, planID, input.Name, input.NewSlug, input.Status)
		if err != nil {
			return EditPlanResponse{}, err
		}
	}
	if plan.ID == 0 {
		// Only goal_body was edited and the helper above set plan; this
		// branch is defensive (plan is always populated when any field
		// was applied) but keeps the projection honest.
		plan, err = svc.GetBySlug(ctx, project, plan.Slug)
		if err != nil {
			return EditPlanResponse{}, err
		}
	}
	return EditPlanResponse{
		Project: projectSummary(project),
		Plan:    planSummary(plan),
	}, nil
}

// DeletePlan hard-deletes a plan after explicit confirmation. The first
// (unconfirmed) call returns a Confirmation block; retry with
// confirmed=true to proceed. Waves cascade; member tasks survive
// detached (plan_id / wave_id cleared).
func (s *Service) DeletePlan(ctx context.Context, input DeletePlanInput) (DeletePlanResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DeletePlanResponse{}, err
	}
	svc := s.newPlanService()
	planID := input.PlanID
	if input.Slug != "" {
		plan, err := svc.GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return DeletePlanResponse{}, err
		}
		planID = plan.ID
	}
	if planID == 0 {
		return DeletePlanResponse{}, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	if !input.Confirmed {
		return DeletePlanResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Deleting a plan is destructive: its waves cascade-delete and member tasks are detached (plan_id / wave_id cleared). The tasks themselves survive. Confirm with confirmed=true to proceed.",
				Options: []ConfirmationOption{
					{Action: "confirm_delete", Label: "Retry plans.delete with confirmed=true to hard-delete the plan"},
				},
			},
		}, nil
	}
	event, err := svc.DeletePlan(ctx, project, planID)
	if err != nil {
		return DeletePlanResponse{}, err
	}
	snapshot := eventSummary(event)
	return DeletePlanResponse{Project: projectSummary(project), Snapshot: &snapshot}, nil
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
		Wave:    planWaveSummary(wave),
	}, nil
}

// planWaveSummary projects a domain.PlanWave into the MCP wire shape.
func planWaveSummary(wave domain.PlanWave) PlanWaveSummary {
	return PlanWaveSummary{
		ID:       wave.ID,
		PlanID:   wave.PlanID,
		Name:     wave.Name,
		Position: wave.Position,
	}
}

// RemovePlanWave deletes a wave after explicit confirmation. The first
// (unconfirmed) call returns a Confirmation block; retry with
// confirmed=true to proceed. The wave's tasks survive with wave_id
// cleared (plan_id intact).
func (s *Service) RemovePlanWave(ctx context.Context, input RemovePlanWaveInput) (RemovePlanWaveResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RemovePlanWaveResponse{}, err
	}
	if input.WaveID == 0 {
		return RemovePlanWaveResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	if !input.Confirmed {
		return RemovePlanWaveResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a wave detaches its tasks (wave_id cleared; they stay in the plan but unscheduled). Confirm with confirmed=true to proceed.",
				Options: []ConfirmationOption{
					{Action: "confirm_remove_wave", Label: "Retry plans.remove_wave with confirmed=true to delete the wave"},
				},
			},
		}, nil
	}
	wave, err := s.newPlanService().RemoveWave(ctx, project, input.WaveID)
	if err != nil {
		return RemovePlanWaveResponse{}, err
	}
	summary := planWaveSummary(wave)
	return RemovePlanWaveResponse{Project: projectSummary(project), Wave: &summary}, nil
}

// RenamePlanWave rewrites a wave's name. Name is required, non-blank, and
// must differ from the current name (else ErrValidation).
func (s *Service) RenamePlanWave(ctx context.Context, input RenamePlanWaveInput) (RenamePlanWaveResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RenamePlanWaveResponse{}, err
	}
	if input.WaveID == 0 {
		return RenamePlanWaveResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	wave, err := s.newPlanService().RenameWave(ctx, project, input.WaveID, input.Name)
	if err != nil {
		return RenamePlanWaveResponse{}, err
	}
	return RenamePlanWaveResponse{Project: projectSummary(project), Wave: planWaveSummary(wave)}, nil
}

// ReorderPlanWave moves a wave to a 1-based position within its plan,
// swapping with the occupant on collision.
func (s *Service) ReorderPlanWave(ctx context.Context, input ReorderPlanWaveInput) (ReorderPlanWaveResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ReorderPlanWaveResponse{}, err
	}
	if input.WaveID == 0 {
		return ReorderPlanWaveResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	wave, err := s.newPlanService().ReorderWave(ctx, project, input.WaveID, input.Position)
	if err != nil {
		return ReorderPlanWaveResponse{}, err
	}
	return ReorderPlanWaveResponse{Project: projectSummary(project), Wave: planWaveSummary(wave)}, nil
}

// UnassignPlanTask detaches a task from its plan (clears plan_id and
// wave_id). Detached=false when the task was already unattached (no-op,
// no event emitted).
func (s *Service) UnassignPlanTask(ctx context.Context, input UnassignPlanTaskInput) (UnassignPlanTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return UnassignPlanTaskResponse{}, err
	}
	if input.TaskID == 0 {
		return UnassignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "task_id is required", nil)
	}
	event, err := s.newPlanService().UnassignTask(ctx, project, input.TaskID)
	if err != nil {
		return UnassignPlanTaskResponse{}, err
	}
	return UnassignPlanTaskResponse{
		Project:  projectSummary(project),
		TaskID:   input.TaskID,
		Detached: event.EventType != "",
	}, nil
}

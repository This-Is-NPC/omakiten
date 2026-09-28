package operation

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/contract"
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
func (s *Service) CreatePlan(ctx context.Context, input contract.CreatePlanInput) (contract.CreatePlanResponse, error) {
	if err := s.allow("plan.create"); err != nil {
		return contract.CreatePlanResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.CreatePlanResponse{}, err
	}
	plan, err := s.newPlanService().Create(ctx, project, input.Slug, input.Name, input.GoalBody)
	if err != nil {
		return contract.CreatePlanResponse{}, err
	}
	return contract.CreatePlanResponse{
		Project: projectSummary(project),
		Plan:    planSummary(plan),
	}, nil
}

// ListPlans returns every plan in the resolved project, oldest first.
// GoalBody is stripped from each entry to keep payloads compact — the
// goal body is full markdown capped at the domain write boundary.
func (s *Service) ListPlans(ctx context.Context, input contract.ListPlansInput) (contract.ListPlansResponse, error) {
	if err := s.allow("plan.list"); err != nil {
		return contract.ListPlansResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ListPlansResponse{}, err
	}
	plans, err := s.newPlanService().List(ctx, project)
	if err != nil {
		return contract.ListPlansResponse{}, err
	}
	summaries := make([]contract.PlanSummary, 0, len(plans))
	for _, p := range plans {
		s := planSummary(p)
		s.GoalBody = ""
		summaries = append(summaries, s)
	}
	return contract.ListPlansResponse{
		Project: projectSummary(project),
		Plans:   summaries,
	}, nil
}

// ShowPlan returns the aggregated plan view: full plan row, waves with
// their task lists, and done/total counts (per wave and overall) plus
// the integer percent (done*100/total, clamped to 0 when total is 0).
// The active wave id is the lowest-position wave that still has pending
// tasks; 0 when every wave is fully done.
func (s *Service) ShowPlan(ctx context.Context, input contract.ShowPlanInput) (contract.ShowPlanResponse, error) {
	if err := s.allow("plan.show"); err != nil {
		return contract.ShowPlanResponse{}, err
	}
	return s.showPlan(ctx, input)
}

func (s *Service) showPlan(ctx context.Context, input contract.ShowPlanInput) (contract.ShowPlanResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ShowPlanResponse{}, err
	}
	show, err := s.newPlanService().Show(ctx, project, input.Slug)
	if err != nil {
		return contract.ShowPlanResponse{}, err
	}

	waves := make([]contract.PlanWaveView, 0, len(show.Waves))
	for _, w := range show.Waves {
		view := contract.PlanWaveView{
			ID:         w.Wave.ID,
			Name:       w.Wave.Name,
			Position:   w.Wave.Position,
			DoneCount:  w.DoneCount,
			TotalCount: w.TotalCount,
		}
		for _, t := range w.Tasks {
			row := contract.PlanTaskRow{
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

	return contract.ShowPlanResponse{
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
func (s *Service) ContinuePlan(ctx context.Context, input contract.ContinuePlanInput) (contract.ContinuePlanResponse, error) {
	if err := s.allow("plan.continue"); err != nil {
		return contract.ContinuePlanResponse{}, err
	}
	show, err := s.showPlan(ctx, contract.ShowPlanInput(input))
	if err != nil {
		return contract.ContinuePlanResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ContinuePlanResponse{}, err
	}
	resp := contract.ContinuePlanResponse{
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
		return contract.ContinuePlanResponse{}, err
	}
	if ok {
		preview := contract.PlanTaskRow{
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
func (s *Service) EditPlan(ctx context.Context, input contract.EditPlanInput) (contract.EditPlanResponse, error) {
	if err := s.allow("plan.edit"); err != nil {
		return contract.EditPlanResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.EditPlanResponse{}, err
	}
	svc := s.newPlanService()
	planID, err := editPlanID(ctx, svc, project, input)
	if err != nil {
		return contract.EditPlanResponse{}, err
	}
	if err := validateEditPlan(input); err != nil {
		return contract.EditPlanResponse{}, err
	}
	plan, err := s.applyPlanEdits(ctx, svc, project, planID, input)
	if err != nil {
		return contract.EditPlanResponse{}, err
	}
	return contract.EditPlanResponse{
		Project: projectSummary(project),
		Plan:    planSummary(plan),
	}, nil
}

func editPlanID(ctx context.Context, svc *app.PlanService, project domain.ProjectContext, input contract.EditPlanInput) (int64, error) {
	if input.Slug != "" {
		plan, err := svc.GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return 0, err
		}
		return plan.ID, nil
	}
	if input.PlanID == 0 {
		return 0, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	return input.PlanID, nil
}

func validateEditPlan(input contract.EditPlanInput) error {
	if input.Name == nil && input.NewSlug == nil && input.Status == nil && input.GoalBody == nil {
		return domain.NewError(domain.ErrValidation,
			"plans.edit requires at least one of name, new_slug, status, goal_body", nil)
	}
	return nil
}

func (s *Service) applyPlanEdits(ctx context.Context, svc *app.PlanService, project domain.ProjectContext, planID int64, input contract.EditPlanInput) (domain.Plan, error) {
	// UpdatePlan must run BEFORE UpdateGoalBody: UpdatePlan rejects a
	// no-op name/slug/status diff with "changed nothing", and that
	// rejection has to fire before the goal-body write commits + emits.
	var plan domain.Plan
	var err error
	if input.Name != nil || input.NewSlug != nil || input.Status != nil {
		plan, err = svc.UpdatePlan(ctx, project, planID, input.Name, input.NewSlug, input.Status)
		if err != nil {
			return domain.Plan{}, err
		}
	}
	if input.GoalBody != nil {
		plan, err = svc.UpdateGoalBody(ctx, project, planID, *input.GoalBody)
		if err != nil {
			return domain.Plan{}, err
		}
	}
	if plan.ID == 0 {
		// Defensive: every editable field path above populates plan, but
		// keep the projection honest if that ever changes.
		plan, err = svc.GetBySlug(ctx, project, input.Slug)
	}
	return plan, err
}

// DeletePlan hard-deletes a plan after explicit confirmation. The first
// (unconfirmed) call returns a Confirmation block; retry with
// confirmed=true to proceed. Waves cascade; member tasks survive
// detached (plan_id / wave_id cleared).
func (s *Service) DeletePlan(ctx context.Context, input contract.DeletePlanInput) (contract.DeletePlanResponse, error) {
	if err := s.allow("plan.delete"); err != nil {
		return contract.DeletePlanResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.DeletePlanResponse{}, err
	}
	svc := s.newPlanService()
	planID := input.PlanID
	if input.Slug != "" {
		plan, err := svc.GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return contract.DeletePlanResponse{}, err
		}
		planID = plan.ID
	}
	if planID == 0 {
		return contract.DeletePlanResponse{}, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	if !input.Confirmed {
		return contract.DeletePlanResponse{
			Project: projectSummary(project),
			Confirmation: contract.Confirmation{
				RequiresConfirmation: true,
				Reason:               "Deleting a plan is destructive: its waves cascade-delete and member tasks are detached (plan_id / wave_id cleared). The tasks themselves survive. Confirm with confirmed=true to proceed.",
				Options: []contract.ConfirmationOption{
					{Action: "confirm_delete", Label: "Retry plans.delete with confirmed=true to hard-delete the plan"},
				},
			},
		}, nil
	}
	event, err := svc.DeletePlan(ctx, project, planID)
	if err != nil {
		return contract.DeletePlanResponse{}, err
	}
	snapshot := eventSummary(event)
	return contract.DeletePlanResponse{Project: projectSummary(project), Snapshot: &snapshot}, nil
}

// AssignPlanTask attaches an existing task to a plan + wave (census
// slug plan.task.assign). Distinct from AssignTask, which sets
// tasks.assigned_to (task.assign / assignee). The plan is identified
// by slug or plan_id (slug wins when both supplied); wave_id is taken
// verbatim — supplying a wave from a different plan fails with
// ErrPlanWaveNotFound.
func (s *Service) AssignPlanTask(ctx context.Context, input contract.AssignPlanTaskInput) (contract.AssignPlanTaskResponse, error) {
	if err := s.allow("plan.task.assign"); err != nil {
		return contract.AssignPlanTaskResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.AssignPlanTaskResponse{}, err
	}
	planID := input.PlanID
	if input.Slug != "" {
		plan, err := s.newPlanService().GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return contract.AssignPlanTaskResponse{}, err
		}
		planID = plan.ID
	}
	if planID == 0 {
		return contract.AssignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	if input.WaveID == 0 {
		return contract.AssignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	if input.TaskID == 0 {
		return contract.AssignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "task_id is required", nil)
	}
	if err := s.newPlanService().AssignTask(ctx, project, input.TaskID, planID, input.WaveID); err != nil {
		return contract.AssignPlanTaskResponse{}, err
	}
	return contract.AssignPlanTaskResponse{
		Project: projectSummary(project),
		TaskID:  input.TaskID,
		PlanID:  planID,
		WaveID:  input.WaveID,
	}, nil
}

// ClaimNextPlanTask runs the atomic claim primitive. Returns Claimed=false
// (and no Task) when nothing is claimable in the plan's active wave.
func (s *Service) ClaimNextPlanTask(ctx context.Context, input contract.ClaimNextPlanTaskInput) (contract.ClaimNextPlanTaskResponse, error) {
	if err := s.allow("plan.task.claim_next"); err != nil {
		return contract.ClaimNextPlanTaskResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ClaimNextPlanTaskResponse{}, err
	}
	planID := input.PlanID
	if input.Slug != "" {
		plan, err := s.newPlanService().GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return contract.ClaimNextPlanTaskResponse{}, err
		}
		planID = plan.ID
	}
	if planID == 0 {
		return contract.ClaimNextPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "plan id or slug is required", nil)
	}
	task, claimed, err := s.newPlanService().ClaimNext(ctx, project, planID)
	if err != nil {
		return contract.ClaimNextPlanTaskResponse{}, err
	}
	resp := contract.ClaimNextPlanTaskResponse{Project: projectSummary(project), Claimed: claimed}
	if claimed {
		summary := taskSummary(task, s.registry)
		resp.Task = &summary
	}
	return resp, nil
}

// AddPlanWave appends (position=0) or inserts (position>0) a wave onto
// a plan. Slug is the user-facing plan handle; plan_id may be supplied
// instead when the caller already holds it from a previous response.
func (s *Service) AddPlanWave(ctx context.Context, input contract.AddPlanWaveInput) (contract.AddPlanWaveResponse, error) {
	if err := s.allow("plan.wave.add"); err != nil {
		return contract.AddPlanWaveResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.AddPlanWaveResponse{}, err
	}
	planID := input.PlanID
	if planID == 0 {
		plan, err := s.newPlanService().GetBySlug(ctx, project, input.Slug)
		if err != nil {
			return contract.AddPlanWaveResponse{}, err
		}
		planID = plan.ID
	}
	wave, err := s.newPlanService().AddWave(ctx, project, planID, input.Name, input.Position)
	if err != nil {
		return contract.AddPlanWaveResponse{}, err
	}
	return contract.AddPlanWaveResponse{
		Project: projectSummary(project),
		Wave:    planWaveSummary(wave),
	}, nil
}

// planWaveSummary projects a domain.PlanWave into the delivery contract.
func planWaveSummary(wave domain.PlanWave) contract.PlanWaveSummary {
	return contract.PlanWaveSummary{
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
func (s *Service) RemovePlanWave(ctx context.Context, input contract.RemovePlanWaveInput) (contract.RemovePlanWaveResponse, error) {
	if err := s.allow("plan.wave.remove"); err != nil {
		return contract.RemovePlanWaveResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.RemovePlanWaveResponse{}, err
	}
	if input.WaveID == 0 {
		return contract.RemovePlanWaveResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	if !input.Confirmed {
		return contract.RemovePlanWaveResponse{
			Project: projectSummary(project),
			Confirmation: contract.Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a wave detaches its tasks (wave_id cleared; they stay in the plan but unscheduled). Confirm with confirmed=true to proceed.",
				Options: []contract.ConfirmationOption{
					{Action: "confirm_remove_wave", Label: "Retry plans.remove_wave with confirmed=true to delete the wave"},
				},
			},
		}, nil
	}
	wave, err := s.newPlanService().RemoveWave(ctx, project, input.WaveID)
	if err != nil {
		return contract.RemovePlanWaveResponse{}, err
	}
	summary := planWaveSummary(wave)
	return contract.RemovePlanWaveResponse{Project: projectSummary(project), Wave: &summary}, nil
}

// RenamePlanWave rewrites a wave's name. Name is required, non-blank, and
// must differ from the current name (else ErrValidation).
func (s *Service) RenamePlanWave(ctx context.Context, input contract.RenamePlanWaveInput) (contract.RenamePlanWaveResponse, error) {
	if err := s.allow("plan.wave.rename"); err != nil {
		return contract.RenamePlanWaveResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.RenamePlanWaveResponse{}, err
	}
	if input.WaveID == 0 {
		return contract.RenamePlanWaveResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	wave, err := s.newPlanService().RenameWave(ctx, project, input.WaveID, input.Name)
	if err != nil {
		return contract.RenamePlanWaveResponse{}, err
	}
	return contract.RenamePlanWaveResponse{Project: projectSummary(project), Wave: planWaveSummary(wave)}, nil
}

// ReorderPlanWave moves a wave to a 1-based position within its plan,
// swapping with the occupant on collision.
func (s *Service) ReorderPlanWave(ctx context.Context, input contract.ReorderPlanWaveInput) (contract.ReorderPlanWaveResponse, error) {
	if err := s.allow("plan.wave.reorder"); err != nil {
		return contract.ReorderPlanWaveResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ReorderPlanWaveResponse{}, err
	}
	if input.WaveID == 0 {
		return contract.ReorderPlanWaveResponse{}, domain.NewError(domain.ErrValidation, "wave_id is required", nil)
	}
	wave, err := s.newPlanService().ReorderWave(ctx, project, input.WaveID, input.Position)
	if err != nil {
		return contract.ReorderPlanWaveResponse{}, err
	}
	return contract.ReorderPlanWaveResponse{Project: projectSummary(project), Wave: planWaveSummary(wave)}, nil
}

// UnassignPlanTask detaches a task from its plan (clears plan_id and
// wave_id). Detached=false when the task was already unattached (no-op,
// no event emitted).
func (s *Service) UnassignPlanTask(ctx context.Context, input contract.UnassignPlanTaskInput) (contract.UnassignPlanTaskResponse, error) {
	if err := s.allow("plan.task.unassign"); err != nil {
		return contract.UnassignPlanTaskResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.UnassignPlanTaskResponse{}, err
	}
	if input.TaskID == 0 {
		return contract.UnassignPlanTaskResponse{}, domain.NewError(domain.ErrValidation, "task_id is required", nil)
	}
	event, err := s.newPlanService().UnassignTask(ctx, project, input.TaskID)
	if err != nil {
		return contract.UnassignPlanTaskResponse{}, err
	}
	return contract.UnassignPlanTaskResponse{
		Project:  projectSummary(project),
		TaskID:   input.TaskID,
		Detached: event.EventType != "",
	}, nil
}

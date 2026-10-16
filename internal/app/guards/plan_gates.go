package guards

import (
	"context"
	"fmt"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// OperationPlanClaim and OperationPlanComplete are the guard.violated
// `operation` payload values of the plan-level gate checks.
const (
	OperationPlanClaim    = "plan.claim"
	OperationPlanComplete = "plan.complete"
)

// planGate is one gate of the workflow's plans block: its stage, the share
// of done tasks a progress gate guards, and its guards.
type planGate struct {
	stage   domain.PlanStage
	percent int
	guards  []domain.TransitionGuard
}

// transitionPlanGates evaluates the gates of task's plan that a move from
// fromBucketID to toBucketID meets: start when the task leaves the first
// bucket, and, when it enters the final bucket, each progress gate the move
// would leave the plan above plus complete when it is the plan's last open
// task. A task in no plan, or in a plan that is not active, meets none. The
// gates come from the root workflow; the first and final buckets from the
// task's own workflow.
func (e *Evaluator) transitionPlanGates(ctx context.Context, projectID int64, task domain.Task, fromBucketID, toBucketID int64, snap *config.Snapshot) ([]domain.GuardStatus, error) {
	gates := e.planGates()
	if gates == nil {
		return nil, nil
	}
	first, final, ok := snap.Workflow().EdgeBuckets()
	if !ok {
		return nil, nil
	}
	state, inPlan, err := e.repo.TaskPlanGateState(ctx, projectID, task.ID, final.ID)
	if err != nil || !inPlan || state.Status != domain.PlanStatusActive {
		return nil, err
	}
	var met []planGate
	if fromBucketID == first.ID {
		met = append(met, planGate{stage: domain.PlanStageStart, guards: gates.Start.Guards})
	}
	if toBucketID == final.ID && fromBucketID != final.ID && state.Total > 0 {
		done := state.Done + 1
		for _, gate := range gates.Progress {
			if done*100 > gate.Percent*state.Total {
				met = append(met, planGate{stage: domain.PlanStageProgress, percent: gate.Percent, guards: gate.Guards})
			}
		}
		if done >= state.Total {
			met = append(met, planGate{stage: domain.PlanStageComplete, guards: gates.Complete.Guards})
		}
	}
	return e.evaluatePlanGates(ctx, projectID, state, met)
}

// EvaluatePlanStage runs the start gates of an active plan before a claim,
// or its complete gates before an edit to done, recording a plan-scoped
// guard.violated event for each unmet guard.
func (e *Evaluator) EvaluatePlanStage(ctx context.Context, projectID, planID int64, stage domain.PlanStage) error {
	statuses, err := e.PreviewPlanStage(ctx, projectID, planID, stage)
	if err != nil {
		return err
	}
	operation := OperationPlanClaim
	if stage == domain.PlanStageComplete {
		operation = OperationPlanComplete
	}
	var unmet []domain.GuardStatus
	var messages []string
	target := map[string]any{"plan_id": planID, "stage": string(stage)}
	for _, status := range statuses {
		if status.Satisfied {
			continue
		}
		e.EmitViolated(ctx, projectID, domain.EventEntityPlan, planID, operation, status.Type, status.Message, target)
		unmet = append(unmet, status)
		messages = append(messages, status.Message)
	}
	if len(unmet) == 0 {
		return nil
	}
	return violation(unmet, messages, map[string]any{"plan_id": planID})
}

// PreviewPlanStage reports the state of the start or complete gates of an
// active plan without recording events. A plan that is not active meets
// none.
func (e *Evaluator) PreviewPlanStage(ctx context.Context, projectID, planID int64, stage domain.PlanStage) ([]domain.GuardStatus, error) {
	return e.previewPlan(ctx, projectID, planID, func(gates *domain.PlanGates) []planGate {
		if stage == domain.PlanStageComplete {
			return []planGate{{stage: stage, guards: gates.Complete.Guards}}
		}
		return []planGate{{stage: stage, guards: gates.Start.Guards}}
	})
}

// PreviewPlanGates reports the state of every gate of an active plan, start
// first, then each progress share, then complete, without recording events.
// A plan that is not active meets none.
func (e *Evaluator) PreviewPlanGates(ctx context.Context, projectID, planID int64) ([]domain.GuardStatus, error) {
	return e.previewPlan(ctx, projectID, planID, func(gates *domain.PlanGates) []planGate {
		all := []planGate{{stage: domain.PlanStageStart, guards: gates.Start.Guards}}
		for _, gate := range gates.Progress {
			all = append(all, planGate{stage: domain.PlanStageProgress, percent: gate.Percent, guards: gate.Guards})
		}
		return append(all, planGate{stage: domain.PlanStageComplete, guards: gates.Complete.Guards})
	})
}

func (e *Evaluator) previewPlan(ctx context.Context, projectID, planID int64, pick func(*domain.PlanGates) []planGate) ([]domain.GuardStatus, error) {
	gates := e.planGates()
	if gates == nil {
		return nil, nil
	}
	state, err := e.repo.PlanGateState(ctx, projectID, planID, 0)
	if err != nil || state.Status != domain.PlanStatusActive {
		return nil, err
	}
	return e.evaluatePlanGates(ctx, projectID, state, pick(gates))
}

// planGates returns the root workflow's plans block, or nil when it declares
// no gate.
func (e *Evaluator) planGates() *domain.PlanGates {
	if e.snap == nil {
		return nil
	}
	gates := e.snap.Workflow().Plans
	if len(gates.Start.Guards) == 0 && len(gates.Progress) == 0 && len(gates.Complete.Guards) == 0 {
		return nil
	}
	return &gates
}

func (e *Evaluator) evaluatePlanGates(ctx context.Context, projectID int64, state domain.PlanGateState, gates []planGate) ([]domain.GuardStatus, error) {
	var out []domain.GuardStatus
	for _, gate := range gates {
		for _, guard := range gate.guards {
			status, err := e.checkPlanCommentsTagged(ctx, projectID, state, gate, guard)
			if err != nil {
				return nil, err
			}
			out = append(out, e.finish(e.snap, guard, status))
		}
	}
	return out, nil
}

func (e *Evaluator) checkPlanCommentsTagged(ctx context.Context, projectID int64, state domain.PlanGateState, gate planGate, guard domain.TransitionGuard) (domain.GuardStatus, error) {
	required := max(guard.Count, 1)
	count, err := e.repo.CountPlanCommentsTagged(ctx, projectID, state.PlanID, guard.Tag)
	if err != nil {
		return domain.GuardStatus{}, err
	}
	details := map[string]any{"plan_id": state.PlanID, "plan": state.Slug, "stage": string(gate.stage), "count": count, "required": required, "tag": guard.Tag}
	action := "starting it"
	switch gate.stage {
	case domain.PlanStageProgress:
		details["percent"] = gate.percent
		action = fmt.Sprintf("passing %d%% done", gate.percent)
	case domain.PlanStageComplete:
		action = "completing it"
	}
	return domain.GuardStatus{
		Satisfied: count >= required,
		Details:   details,
		Fix:       planCommentFix(ctx, state.Slug, guard.Tag, "<evidence>"),
		Message:   fmt.Sprintf("%s guard: plan %q has %d comment(s) tagged %q; %s requires at least %d", guard.Type, state.Slug, count, guard.Tag, action, required),
	}, nil
}

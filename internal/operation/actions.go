package operation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func invokeAction[I, R any](ctx context.Context, args map[string]any, run func(context.Context, I) (R, error)) (any, error) {
	var input I
	if _, scoped := reflect.TypeFor[I]().FieldByName("ProjectSelector"); !scoped {
		delete(args, "project_id")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, domain.NewError(domain.ErrValidation, "invalid action input", map[string]any{"error": err.Error()})
	}
	return run(ctx, input)
}

// ExecuteAction dispatches a structured notification intent through surface-gated operations.
func (s *Service) ExecuteAction(ctx context.Context, request contract.ActionRequest) (contract.ActionResult, error) {
	args := make(map[string]any, len(request.Arguments)+1)
	for k, v := range request.Arguments {
		args[k] = v
	}
	delete(args, "project")
	args["project_id"] = request.ProjectID
	handler := s.actionHandlers()[request.Operation]
	if handler == nil {
		return contract.ActionResult{}, domain.NewError(domain.ErrValidation, fmt.Sprintf("unknown action operation %q", request.Operation), nil)
	}
	value, err := handler(ctx, args)
	if err != nil {
		return contract.ActionResult{}, err
	}
	result := contract.ActionResult{}
	response := reflect.ValueOf(value)
	if response.Kind() == reflect.Struct {
		if field := response.FieldByName("Confirmation"); field.IsValid() {
			result.Confirmation, _ = field.Interface().(contract.Confirmation)
		}
	}
	if migrated, ok := value.(contract.MigrateOrphansResponse); ok {
		result.Migration = migrated.Applied || migrated.Report.Total == 0
		result.Migrated = migrated.Report.Total
	}
	return result, nil
}

type actionHandler func(context.Context, map[string]any) (any, error)

func bindAction[I, R any](run func(context.Context, I) (R, error)) actionHandler {
	return func(ctx context.Context, args map[string]any) (any, error) { return invokeAction(ctx, args, run) }
}

func (s *Service) actionHandlers() map[string]actionHandler {
	return map[string]actionHandler{
		"command.list": bindAction(func(ctx context.Context, _ struct{}) (contract.ListCommandsResponse, error) {
			return s.ListCommands(ctx)
		}),
		"command.resolve":      bindAction(s.ResolveCommand),
		"comment.add":          bindAction(s.AddComment),
		"comment.delete":       bindAction(s.DeleteComment),
		"comment.edit":         bindAction(s.EditComment),
		"comment.list":         bindAction(s.ListComments),
		"dependency.add":       bindAction(s.AddDependency),
		"dependency.list":      bindAction(s.ListDependencies),
		"dependency.remove":    bindAction(s.RemoveDependency),
		"error.record":         bindAction(s.RecordError),
		"insights.summary":     bindAction(s.InsightsSummary),
		"law.get":              bindAction(s.ShowLaw),
		"law.list":             bindAction(s.ListLaws),
		"logs.list":            bindAction(s.ListLogs),
		"metrics.summary":      bindAction(s.MetricsSummary),
		"orphans.migrate":      bindAction(s.MigrateOrphans),
		"persona.get":          bindAction(s.ShowPersona),
		"persona.list":         bindAction(s.ListPersonas),
		"plan.continue":        bindAction(s.ContinuePlan),
		"plan.create":          bindAction(s.CreatePlan),
		"plan.delete":          bindAction(s.DeletePlan),
		"plan.edit":            bindAction(s.EditPlan),
		"plan.list":            bindAction(s.ListPlans),
		"plan.show":            bindAction(s.ShowPlan),
		"plan.task.assign":     bindAction(s.AssignPlanTask),
		"plan.task.claim_next": bindAction(s.ClaimNextPlanTask),
		"plan.task.unassign":   bindAction(s.UnassignPlanTask),
		"plan.wave.add":        bindAction(s.AddPlanWave),
		"plan.wave.remove":     bindAction(s.RemovePlanWave),
		"plan.wave.rename":     bindAction(s.RenamePlanWave),
		"plan.wave.reorder":    bindAction(s.ReorderPlanWave),
		"progress.record":      bindAction(s.RecordProgress),
		"project.edit":         bindAction(s.EditProject),
		"project.overview":     bindAction(s.Overview),
		"project.resume":       bindAction(s.ResumeProject),
		"search":               bindAction(s.Search),
		"skill.get":            bindAction(s.ShowSkill),
		"skill.list":           bindAction(s.ListSkills),
		"solution.add":         bindAction(s.AddSolution),
		"solution.confirm":     bindAction(s.ConfirmSolution),
		"solution.list_top":    bindAction(s.ListTopSolutions),
		"tag.add":              bindAction(s.AddTag),
		"tag.list":             bindAction(s.ListTags),
		"tag.list_all": bindAction(func(ctx context.Context, _ struct{}) (contract.AllTagsResponse, error) {
			return s.ListAllTags(ctx)
		}),
		"tag.merge":          bindAction(s.MergeTags),
		"tag.remove":         bindAction(s.RemoveTag),
		"task.archive":       bindAction(s.ArchiveTask),
		"task.assign":        bindAction(s.AssignTask),
		"task.continue":      bindAction(s.ContinueTask),
		"task.create":        bindAction(s.CreateTask),
		"task.create_intent": bindAction(s.CreateTaskIntent),
		"task.delete":        bindAction(s.DeleteTask),
		"task.edit":          bindAction(s.EditTask),
		"task.list":          bindAction(s.ListTasks),
		"task.transition":    bindAction(s.MoveTask),
		"task.unarchive":     bindAction(s.UnarchiveTask),
		"task_activity.list": bindAction(s.ListTaskActivity),
		"template.list":      bindAction(s.ListTemplates),
		"template.show":      bindAction(s.ShowTemplate),
		"workflow.show":      bindAction(s.ShowWorkflow),
	}
}

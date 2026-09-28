package operation

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func (s *Service) ImportPlan(ctx context.Context, input contract.ImportWorkInput) (contract.ImportWorkResponse, error) {
	if err := s.allow("plan.import"); err != nil {
		return contract.ImportWorkResponse{}, err
	}
	return s.importWork(ctx, input, "Omakiten Plan")
}

func (s *Service) ImportTask(ctx context.Context, input contract.ImportWorkInput) (contract.ImportWorkResponse, error) {
	if err := s.allow("task.import"); err != nil {
		return contract.ImportWorkResponse{}, err
	}
	return s.importWork(ctx, input, "Omakiten Task")
}

func (s *Service) workDocumentService() (*app.WorkDocumentService, error) {
	repo, ok := s.repo.(app.DocumentRepository)
	if !ok {
		return nil, domain.NewError(domain.ErrValidation, "document repository is unavailable", nil)
	}
	return app.NewWorkDocumentService(repo, s.snapshot), nil
}

func (s *Service) importWork(ctx context.Context, input contract.ImportWorkInput, kind string) (contract.ImportWorkResponse, error) {
	var response contract.ImportWorkResponse
	if input.Document.Type != kind {
		return response, domain.NewError(domain.ErrValidation, "unexpected document type", nil)
	}
	if err := app.ValidateWorkDocument(input.Document); err != nil {
		return response, err
	}
	if err := s.authorizeWorkImport(input.Document); err != nil {
		return response, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return response, err
	}
	response.Project = projectSummary(project)
	if kind == "Omakiten Task" && !input.Confirmed {
		task := input.Document.Spec.Task
		if similar, found, err := s.similarTaskConfirmation(ctx, project, task.Title, task.Description, nil); err != nil {
			return response, err
		} else if found {
			response.Confirmation, response.SimilarTasks = similar.Confirmation, similar.SimilarTasks
			return response, nil
		}
	}
	service, err := s.workDocumentService()
	if err != nil {
		return response, err
	}
	result, err := service.Import(ctx, project, input.Document, input.DryRun)
	response.Plan, response.Tasks = result.Plan, result.Tasks
	response.TaskCount, response.WaveCount, response.DryRun = result.TaskCount, result.WaveCount, result.DryRun
	return response, err
}

func (s *Service) authorizeWorkImport(doc domain.WorkDocument) error {
	ops := map[string]bool{}
	if doc.Type == "Omakiten Plan" {
		ops["plan.create"] = true
	}
	if doc.Spec.Status != "" && doc.Spec.Status != "active" {
		ops["plan.edit"] = true
	}
	if len(doc.Spec.Waves) > 0 {
		ops["plan.wave.add"] = true
	}
	tasks := doc.TaskList()
	for _, task := range tasks {
		ops["task.create"] = true
		if doc.Type == "Omakiten Plan" {
			ops["plan.task.assign"] = true
		}
		for _, op := range workTaskOperations(task) {
			ops[op] = true
		}
	}
	for op := range ops {
		if err := s.allow(op); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ExportPlan(ctx context.Context, input contract.ExportWorkInput) (domain.WorkDocument, error) {
	if err := s.allow("plan.export"); err != nil {
		return domain.WorkDocument{}, err
	}
	if input.Slug == "" {
		return domain.WorkDocument{}, domain.NewError(domain.ErrValidation, "plan slug is required", nil)
	}
	return s.exportWork(ctx, input)
}

func (s *Service) ExportTask(ctx context.Context, input contract.ExportWorkInput) (domain.WorkDocument, error) {
	if err := s.allow("task.export"); err != nil {
		return domain.WorkDocument{}, err
	}
	if input.TaskID <= 0 {
		return domain.WorkDocument{}, domain.NewError(domain.ErrValidation, "task id must be positive", nil)
	}
	return s.exportWork(ctx, input)
}

func (s *Service) exportWork(ctx context.Context, input contract.ExportWorkInput) (domain.WorkDocument, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return domain.WorkDocument{}, err
	}
	service, err := s.workDocumentService()
	if err != nil {
		return domain.WorkDocument{}, err
	}
	return service.Export(ctx, project, input.Slug, input.TaskID)
}

func workTaskOperations(task domain.WorkTask) []string {
	var ops []string
	if len(task.DependsOn) > 0 {
		ops = append(ops, "dependency.add")
	}
	if len(task.Tags) > 0 {
		ops = append(ops, "tag.add")
	}
	if task.Assignee != "" {
		ops = append(ops, "task.assign")
	}
	if task.State == "archived" {
		ops = append(ops, "task.archive")
	}
	return ops
}

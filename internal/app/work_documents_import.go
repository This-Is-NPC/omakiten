package app

import (
	"context"
	"encoding/json"
	"errors"

	"omakiten/internal/domain"
)

var errDryRunRollback = errors.New("document preview rollback")

func (s *WorkDocumentService) Import(ctx context.Context, project domain.ProjectContext, doc domain.WorkDocument, dryRun bool) (WorkImportResult, error) {
	result := WorkImportResult{TaskCount: len(doc.TaskList()), WaveCount: len(doc.Spec.Waves), DryRun: dryRun}
	if err := ValidateWorkDocument(doc); err != nil {
		return result, err
	}
	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.importWork(txCtx, project, doc, &result); err != nil {
			return err
		}
		if dryRun {
			return errDryRunRollback
		}
		return nil
	})
	if errors.Is(err, errDryRunRollback) {
		result.Plan, result.Tasks, err = nil, nil, nil
	}
	if err != nil {
		result.Plan, result.Tasks = nil, nil
	}
	return result, err
}

func (s *WorkDocumentService) importWork(ctx context.Context, project domain.ProjectContext, doc domain.WorkDocument, result *WorkImportResult) error {
	metadata := workMetadata{Waves: map[int64]domain.WorkWave{}}
	waveIDs := map[string]int64{}
	if doc.Type == "Omakiten Plan" {
		plan, err := NewPlanServiceWithSnapshot(s.repo, s.snap).Create(ctx, project, doc.Spec.Slug, doc.Title, doc.Body)
		if err != nil {
			return err
		}
		result.Plan = &plan
		for i, wave := range doc.Spec.Waves {
			created, err := NewPlanServiceWithSnapshot(s.repo, s.snap).AddWave(ctx, project, plan.ID, wave.Name, i+1)
			if err != nil {
				return err
			}
			for _, task := range wave.Tasks {
				waveIDs[task.Key] = created.ID
			}
			wave.Name, wave.Tasks = "", nil
			metadata.Waves[created.ID] = wave
		}
	}
	if err := s.importTasks(ctx, project, doc, waveIDs, result); err != nil {
		return err
	}
	return s.finishWorkImport(ctx, project, doc, metadata, result)
}

func (s *WorkDocumentService) importTasks(ctx context.Context, project domain.ProjectContext, doc domain.WorkDocument, waveIDs map[string]int64, result *WorkImportResult) error {
	service := NewTaskServiceFromStore(s.repo, s.snap.Registry(), s.snap)
	result.Tasks = map[string]int64{}
	pending := doc.TaskList()
	for len(pending) > 0 {
		next := []domain.WorkTask{}
		for _, item := range pending {
			if item.Parent != "" && result.Tasks[item.Parent] == 0 {
				next = append(next, item)
				continue
			}
			if err := s.importMember(ctx, project, service, item, waveIDs[item.Key], result); err != nil {
				return err
			}
		}
		if len(next) == len(pending) {
			return documentError("unresolved task parents")
		}
		pending = next
	}
	if err := s.importDependencies(ctx, project, doc.TaskList(), result.Tasks); err != nil {
		return err
	}
	return s.restoreTaskStates(ctx, project, service, doc.TaskList(), result.Tasks)
}

func (s *WorkDocumentService) importMember(ctx context.Context, project domain.ProjectContext, service *TaskService, item domain.WorkTask, waveID int64, result *WorkImportResult) error {
	task, err := s.importTask(ctx, project, service, item, result.Tasks)
	if err != nil {
		return err
	}
	result.Tasks[item.Key] = task.ID
	if result.Plan == nil || (item.PlanMember != nil && !*item.PlanMember) {
		return nil
	}
	return s.repo.AssignTaskToPlan(ctx, project.ID, task.ID, result.Plan.ID, waveID)
}

func (s *WorkDocumentService) importDependencies(ctx context.Context, project domain.ProjectContext, tasks []domain.WorkTask, ids map[string]int64) error {
	for _, item := range tasks {
		for _, key := range item.DependsOn {
			if _, err := NewDependencyServiceWithEvents(s.repo, s.repo).Add(ctx, project, ids[item.Key], ids[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *WorkDocumentService) importTask(ctx context.Context, project domain.ProjectContext, service *TaskService, item domain.WorkTask, ids map[string]int64) (domain.Task, error) {
	var task domain.Task
	var err error
	if item.Parent == "" {
		task, err = service.Add(ctx, project, item.Title, item.Description, item.Priority, item.Bucket)
	} else {
		task, err = service.AddSub(ctx, project, ids[item.Parent], item.Title, item.Description, item.Priority, item.Bucket)
	}
	if err != nil {
		return task, err
	}
	for _, tag := range item.Tags {
		if _, err := NewTagServiceWithEvents(s.repo, s.repo, s.snap).Add(ctx, project, TagEntityTask, task.ID, tag); err != nil {
			return task, err
		}
	}
	if item.Assignee != "" {
		if _, _, err := service.Assign(ctx, project, task.ID, item.Assignee); err != nil {
			return task, err
		}
	}
	item = domain.WorkTask{Key: item.Key, Metadata: item.Metadata}
	data, err := json.Marshal(workMetadata{Task: item})
	if err != nil {
		return task, err
	}
	return task, s.repo.SaveWorkMetadata(ctx, "task", task.ID, data)
}

func (s *WorkDocumentService) restoreTaskStates(ctx context.Context, project domain.ProjectContext, service *TaskService, tasks []domain.WorkTask, ids map[string]int64) error {
	for _, task := range tasks {
		if task.State == "archived" {
			if _, _, err := service.Archive(ctx, project, ids[task.Key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *WorkDocumentService) finishWorkImport(ctx context.Context, project domain.ProjectContext, doc domain.WorkDocument, metadata workMetadata, result *WorkImportResult) error {
	kind, id := "task", int64(0)
	if doc.Spec.Task != nil {
		id = result.Tasks[doc.Spec.Task.Key]
		metadata.Task = domain.WorkTask{Key: doc.Spec.Task.Key, Metadata: doc.Spec.Task.Metadata}
	}
	if result.Plan != nil {
		kind, id = "plan", result.Plan.ID
		if doc.Spec.Status != "" && doc.Spec.Status != "active" {
			plan, err := s.repo.UpdatePlan(ctx, project.ID, id, nil, nil, &doc.Spec.Status)
			if err != nil {
				return err
			}
			result.Plan = &plan
		}
	}
	doc.Body, doc.Title = "", ""
	doc.Spec.Task, doc.Spec.Waves, doc.Spec.Tasks = nil, nil, nil
	doc.Spec.Slug, doc.Spec.Status = "", ""
	metadata.Document = &doc
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return s.repo.SaveWorkMetadata(ctx, kind, id, data)
}

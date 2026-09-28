package app

import (
	"context"
	"fmt"

	"omakiten/internal/domain"
)

func (s *WorkDocumentService) Export(ctx context.Context, project domain.ProjectContext, slug string, taskID int64) (domain.WorkDocument, error) {
	var doc domain.WorkDocument
	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		record, err := s.repo.ReadWorkRecord(txCtx, project.ID, slug, taskID, s.snap)
		if err != nil {
			return err
		}
		doc, err = s.exportWork(record, taskID)
		return err
	})
	return doc, err
}

func (s *WorkDocumentService) exportWork(record domain.WorkRecord, taskID int64) (domain.WorkDocument, error) {
	doc, waveIndexes, err := initializeWorkExport(record)
	if err != nil {
		return doc, err
	}
	tasks, err := s.exportTasks(record)
	if err != nil {
		return doc, err
	}
	for _, row := range record.Tasks {
		item := tasks[row.Task.ID]
		switch {
		case row.Task.ID == taskID:
			doc.Type, doc.Title, doc.Body, doc.Spec.Task = "Omakiten Task", item.Title, item.Description, &item
			root, err := decodeWorkMetadata(row.Metadata)
			if err != nil {
				return doc, err
			}
			if root.Document != nil {
				doc.Metadata, doc.Description, doc.Spec.Metadata = root.Document.Metadata, root.Document.Description, root.Document.Spec.Metadata
			}
		case row.WaveID != 0:
			i, ok := waveIndexes[row.WaveID]
			if !ok {
				return doc, documentError("task references an unavailable wave")
			}
			doc.Spec.Waves[i].Tasks = append(doc.Spec.Waves[i].Tasks, item)
		default:
			doc.Spec.Tasks = append(doc.Spec.Tasks, item)
		}
	}
	return doc, ValidateWorkDocument(doc)
}

func (s *WorkDocumentService) exportTasks(record domain.WorkRecord) (map[int64]domain.WorkTask, error) {
	tasks := map[int64]domain.WorkTask{}
	items := make([]domain.WorkTask, len(record.Tasks))
	keys := make([]string, len(record.Tasks))
	for i, row := range record.Tasks {
		metadata, err := decodeWorkMetadata(row.Metadata)
		if err != nil {
			return nil, err
		}
		items[i], keys[i] = metadata.Task, metadata.Task.Key
	}
	keys, err := allocateWorkKeys(keys, "task")
	if err != nil {
		return nil, err
	}
	for i, row := range record.Tasks {
		item := items[i]
		item.Key = keys[i]
		item.Title, item.Description = row.Task.Title, row.Task.Description
		bucket, ok := s.snap.For(row.Task).BucketByID(row.Task.BucketID)
		if !ok {
			return nil, documentError("task bucket is unavailable in the active workflow")
		}
		item.Priority, item.Bucket = s.snap.Registry().PriorityLabel(row.Task.Priority), bucket.Key
		if item.Priority == "" {
			return nil, documentError("task priority is unavailable in the active configuration")
		}
		item.State, item.Assignee, item.Tags = string(row.Task.State), row.Assignee, row.Tags
		if record.Plan != nil && row.PlanID == 0 {
			member := false
			item.PlanMember = &member
		}
		tasks[row.Task.ID] = item
	}
	if err := linkExportTasks(tasks, record); err != nil {
		return nil, err
	}
	return tasks, nil
}

func linkExportTasks(tasks map[int64]domain.WorkTask, record domain.WorkRecord) error {
	for _, row := range record.Tasks {
		if row.Task.ParentID == nil {
			continue
		}
		parent, ok := tasks[*row.Task.ParentID]
		if !ok {
			return documentError("parent task is outside the exported document")
		}
		item := tasks[row.Task.ID]
		item.Parent = parent.Key
		tasks[row.Task.ID] = item
	}
	for _, edge := range record.Dependencies {
		item, from := tasks[edge.TaskID]
		target, to := tasks[edge.DependsOnTaskID]
		if from && !to {
			return documentError("dependency crosses the exported document boundary")
		}
		if from {
			item.DependsOn = append(item.DependsOn, target.Key)
			tasks[edge.TaskID] = item
		}
	}
	return nil
}

func initializeWorkExport(record domain.WorkRecord) (domain.WorkDocument, map[int64]int, error) {
	metadata, err := decodeWorkMetadata(record.Metadata)
	if err != nil {
		return domain.WorkDocument{}, nil, err
	}
	doc := domain.WorkDocument{Spec: domain.WorkSpec{Version: 1}}
	if metadata.Document != nil {
		doc = *metadata.Document
	}
	if record.Plan != nil {
		doc.Type, doc.Title, doc.Body = "Omakiten Plan", record.Plan.Name, record.Plan.GoalBody
		doc.Spec.Slug, doc.Spec.Status = record.Plan.Slug, string(record.Plan.Status)
	}
	keys := make([]string, len(record.Waves))
	for i, wave := range record.Waves {
		keys[i] = metadata.Waves[wave.ID].Key
	}
	keys, err = allocateWorkKeys(keys, "wave")
	if err != nil {
		return doc, nil, err
	}
	waveIndexes := map[int64]int{}
	for i, wave := range record.Waves {
		item := metadata.Waves[wave.ID]
		item.Key = keys[i]
		item.Name, item.Tasks = wave.Name, nil
		waveIndexes[wave.ID] = len(doc.Spec.Waves)
		doc.Spec.Waves = append(doc.Spec.Waves, item)
	}
	return doc, waveIndexes, nil
}

func allocateWorkKeys(keys []string, prefix string) ([]string, error) {
	used := map[string]bool{}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if used[key] {
			return nil, documentError("document reference collision: " + key)
		}
		used[key] = true
	}
	candidate := 1
	for i, key := range keys {
		if key != "" {
			continue
		}
		for {
			key = fmt.Sprintf("%s-%d", prefix, candidate)
			candidate++
			if !used[key] {
				break
			}
		}
		keys[i], used[key] = key, true
	}
	return keys, nil
}

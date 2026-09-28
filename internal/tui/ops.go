package tui

import (
	"context"
	"fmt"

	"omakiten/internal/cliutil"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
)

func (m *Model) ops() *operation.Service {
	return m.repos.operationService().ForTUI()
}

func (m *Model) requireOps() (*operation.Service, bool) {
	svc := m.ops()
	if svc == nil {
		m.status = m.t("tui.status.operation_unavailable")
		return nil, false
	}
	return svc, true
}

func (m *Model) projectSelector() operation.ProjectSelector {
	return operation.ProjectSelector{ProjectID: m.project.ID}
}

func (m Model) loadBoardSnapshot(ctx context.Context, project domain.ProjectContext, sort domain.TaskSort, archived bool) (operation.BoardSnapshot, error) {
	if svc := m.repos.operationService(); svc == nil {
		return operation.BoardSnapshot{}, nil
	} else {
		return svc.BoardSnapshot(ctx, operation.BoardSnapshotInput{
			ProjectSelector: operation.ProjectSelector{ProjectID: project.ID},
			Sort:            sort,
			IncludeArchived: archived})
	}
}

func (m Model) loadPlanRollups(ctx context.Context, project domain.ProjectContext) ([]domain.PlanRollup, error) {
	svc := m.repos.operationService()
	if svc == nil {
		return nil, nil
	}
	return svc.ListPlanRollups(ctx, operation.ProjectSelector{ProjectID: project.ID})
}

func (m Model) loadPlanShow(ctx context.Context, project domain.ProjectContext, slug string) (domain.PlanShow, error) {
	svc := m.repos.operationService()
	if svc == nil {
		return domain.PlanShow{}, fmt.Errorf("operation service is not wired")
	}
	return svc.ShowPlanView(ctx, operation.ShowPlanInput{
		ProjectSelector: operation.ProjectSelector{ProjectID: project.ID},
		Slug:            slug})
}

func resolveEditor() string {
	return cliutil.ResolveEditor()
}

func (m Model) loadTaskByID(taskID int64) (domain.Task, error) {
	if m.repos.Tasks == nil {
		return domain.Task{}, fmt.Errorf("task store is not wired")
	}
	return m.repos.Tasks.GetTaskByID(m.ctx, m.project.ID, taskID, m.repos.activeSnapshot())
}

func (m *Model) moveTask(taskID int64, bucketKey string) error {
	svc, ok := m.requireOps()
	if !ok {
		return fmt.Errorf("%s", m.status)
	}
	_, err := svc.MoveTask(m.ctx, operation.MoveTaskInput{
		ProjectSelector: m.projectSelector(),
		TaskID:          taskID,
		BucketKey:       bucketKey})
	return err
}

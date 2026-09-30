package httpapi

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// fakeOps records every operation call. A hook replaces the recording for
// tests that shape one operation's result.
type fakeOps struct {
	calls      []opCall
	listTasks  func(contract.ListTasksInput) (contract.ListTasksResponse, error)
	createTask func(contract.CreateTaskInput) (contract.CreateTaskResponse, error)
	listLogs   func(contract.ListLogsInput) (contract.ListLogsResponse, error)
}

type opCall struct {
	method string
	input  any
}

func (f *fakeOps) record(method string, input any) {
	f.calls = append(f.calls, opCall{method: method, input: input})
}

func (f *fakeOps) ListProjects(context.Context) ([]domain.Project, error) {
	f.record("ListProjects", nil)
	return []domain.Project{{ID: 7, Name: "Alpha", Slug: "alpha"}}, nil
}

func (f *fakeOps) Overview(_ context.Context, in contract.OverviewInput) (contract.OverviewResponse, error) {
	f.record("Overview", in)
	return contract.OverviewResponse{}, nil
}

func (f *fakeOps) ResumeProject(_ context.Context, in contract.ResumeProjectInput) (contract.ResumeProjectResponse, error) {
	f.record("ResumeProject", in)
	return contract.ResumeProjectResponse{}, nil
}

func (f *fakeOps) ShowWorkflow(_ context.Context, in contract.WorkflowInput) (contract.WorkflowResponse, error) {
	f.record("ShowWorkflow", in)
	return contract.WorkflowResponse{}, nil
}

func (f *fakeOps) TaskBoard(_ context.Context, in contract.TaskBoardInput) (contract.TaskBoardResponse, error) {
	f.record("TaskBoard", in)
	return contract.TaskBoardResponse{}, nil
}

func (f *fakeOps) ListTasks(_ context.Context, in contract.ListTasksInput) (contract.ListTasksResponse, error) {
	if f.listTasks != nil {
		return f.listTasks(in)
	}
	f.record("ListTasks", in)
	return contract.ListTasksResponse{}, nil
}

func (f *fakeOps) ShowTask(_ context.Context, in contract.ShowTaskInput) (contract.ShowTaskResponse, error) {
	f.record("ShowTask", in)
	return contract.ShowTaskResponse{}, nil
}

func (f *fakeOps) CreateTaskIntent(_ context.Context, in contract.CreateTaskInput) (contract.CreateTaskResponse, error) {
	if f.createTask != nil {
		return f.createTask(in)
	}
	f.record("CreateTaskIntent", in)
	return contract.CreateTaskResponse{}, nil
}

func (f *fakeOps) EditTask(_ context.Context, in contract.EditTaskInput) (contract.EditTaskResponse, error) {
	f.record("EditTask", in)
	return contract.EditTaskResponse{}, nil
}

func (f *fakeOps) MoveTask(_ context.Context, in contract.MoveTaskInput) (contract.MoveTaskResponse, error) {
	f.record("MoveTask", in)
	return contract.MoveTaskResponse{}, nil
}

func (f *fakeOps) AssignTask(_ context.Context, in contract.AssignTaskInput) (contract.AssignTaskResponse, error) {
	f.record("AssignTask", in)
	return contract.AssignTaskResponse{}, nil
}

func (f *fakeOps) ListComments(_ context.Context, in contract.ListCommentsInput) (contract.CommentsResponse, error) {
	f.record("ListComments", in)
	return contract.CommentsResponse{}, nil
}

func (f *fakeOps) AddComment(_ context.Context, in contract.AddCommentInput) (contract.CommentResponse, error) {
	f.record("AddComment", in)
	return contract.CommentResponse{}, nil
}

func (f *fakeOps) ListTaskActivity(_ context.Context, in contract.ListTaskActivityInput) (contract.ListTaskActivityResponse, error) {
	f.record("ListTaskActivity", in)
	return contract.ListTaskActivityResponse{}, nil
}

func (f *fakeOps) ListDependencies(_ context.Context, in contract.ListDependenciesInput) (contract.DependenciesResponse, error) {
	f.record("ListDependencies", in)
	return contract.DependenciesResponse{}, nil
}

func (f *fakeOps) ListPlans(_ context.Context, in contract.ListPlansInput) (contract.ListPlansResponse, error) {
	f.record("ListPlans", in)
	return contract.ListPlansResponse{}, nil
}

func (f *fakeOps) ShowPlan(_ context.Context, in contract.ShowPlanInput) (contract.ShowPlanResponse, error) {
	f.record("ShowPlan", in)
	return contract.ShowPlanResponse{}, nil
}

func (f *fakeOps) Search(_ context.Context, in contract.SearchInput) (contract.SearchResponse, error) {
	f.record("Search", in)
	return contract.SearchResponse{}, nil
}

func (f *fakeOps) ListLogs(_ context.Context, in contract.ListLogsInput) (contract.ListLogsResponse, error) {
	if f.listLogs != nil {
		return f.listLogs(in)
	}
	f.record("ListLogs", in)
	return contract.ListLogsResponse{}, nil
}

func (f *fakeOps) InsightsSummary(_ context.Context, in contract.InsightsSummaryInput) (contract.InsightsSummaryResponse, error) {
	f.record("InsightsSummary", in)
	return contract.InsightsSummaryResponse{}, nil
}

func (f *fakeOps) MetricsSummary(_ context.Context, in contract.MetricsSummaryInput) (contract.MetricsSummaryResponse, error) {
	f.record("MetricsSummary", in)
	return contract.MetricsSummaryResponse{}, nil
}

type fakeRuntimes struct {
	ops       *fakeOps
	projects  map[string]int64
	catalog   *config.Catalog
	knowledge domain.KnowledgeSnapshot
	snapshot  *config.Snapshot
}

func (f fakeRuntimes) Project(_ context.Context, slug string) (Operations, contract.ProjectSelector, error) {
	id, ok := f.projects[slug]
	if !ok {
		return nil, contract.ProjectSelector{}, errProjectNotFound()
	}
	return f.ops, contract.ProjectSelector{ProjectID: id}, nil
}

func (f fakeRuntimes) Knowledge(_ context.Context, slug string) (domain.KnowledgeSnapshot, error) {
	if _, ok := f.projects[slug]; !ok {
		return domain.KnowledgeSnapshot{}, errProjectNotFound()
	}
	return f.knowledge, nil
}

func (f fakeRuntimes) Snapshot(_ context.Context, slug string) (*config.Snapshot, error) {
	if _, ok := f.projects[slug]; !ok {
		return nil, errProjectNotFound()
	}
	return f.snapshot, nil
}

func errProjectNotFound() error {
	return domain.NewError(domain.ErrProjectNotFound, "project not found", nil)
}

func (f fakeRuntimes) Global(context.Context) (Operations, error) { return f.ops, nil }

func (f fakeRuntimes) Catalog() *config.Catalog { return f.catalog }

type fakeLog struct {
	rows []contract.LogsRow
	err  error
}

func (f fakeLog) EventsAfter(_ context.Context, afterID int64, limit int) ([]contract.LogsRow, error) {
	var out []contract.LogsRow
	for _, row := range f.rows {
		if row.ID > afterID && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, f.err
}

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

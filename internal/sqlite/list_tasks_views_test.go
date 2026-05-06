package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestListTasksHonorsSortField(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithProject(ctx, t)

	// Create three tasks with deliberately non-id-sorted titles so sort
	// behaviour is observable beyond the legacy "by id ascending" path.
	for _, tc := range []struct{ title, priority string }{
		{"charlie", "high"},
		{"alpha", "low"},
		{"bravo", "normal"},
	} {
		if _, err := store.CreateTask(ctx, project.ID, tc.title, "", tc.priority, "backlog"); err != nil {
			t.Fatalf("CreateTask(%s) = %v", tc.title, err)
		}
	}

	cases := []struct {
		name  string
		sort  domain.TaskSort
		want  []string
	}{
		{"id asc", domain.TaskSort{Field: "id", Order: "asc"}, []string{"charlie", "alpha", "bravo"}},
		{"id desc", domain.TaskSort{Field: "id", Order: "desc"}, []string{"bravo", "alpha", "charlie"}},
		{"title asc", domain.TaskSort{Field: "title", Order: "asc"}, []string{"alpha", "bravo", "charlie"}},
		{"title desc", domain.TaskSort{Field: "title", Order: "desc"}, []string{"charlie", "bravo", "alpha"}},
		{"priority asc", domain.TaskSort{Field: "priority", Order: "asc"}, []string{"alpha", "bravo", "charlie"}},
		{"priority desc", domain.TaskSort{Field: "priority", Order: "desc"}, []string{"charlie", "bravo", "alpha"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{Sort: tc.sort})
			if err != nil {
				t.Fatalf("ListTasks() = %v", err)
			}
			got := make([]string, 0, len(tasks))
			for _, task := range tasks {
				got = append(got, task.Title)
			}
			if !equalStrings(got, tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListTasksHonorsPriorityFilter(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithProject(ctx, t)

	for _, tc := range []struct{ title, priority string }{
		{"alpha", "low"},
		{"bravo", "normal"},
		{"charlie", "high"},
	} {
		if _, err := store.CreateTask(ctx, project.ID, tc.title, "", tc.priority, "backlog"); err != nil {
			t.Fatalf("CreateTask(%s) = %v", tc.title, err)
		}
	}

	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{
		Priorities: []domain.Priority{domain.PriorityHigh, domain.PriorityLow},
	})
	if err != nil {
		t.Fatalf("ListTasks() = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len = %d, want 2 (low + high)", len(tasks))
	}
	for _, task := range tasks {
		if task.Priority != domain.PriorityLow && task.Priority != domain.PriorityHigh {
			t.Errorf("unexpected priority %q in filtered result", task.Priority)
		}
	}
}

func TestListTasksHonorsBucketKeysFilter(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithProject(ctx, t)

	a, err := store.CreateTask(ctx, project.ID, "alpha", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(alpha) = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "bravo", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask(bravo) = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, a.ID, "dev"); err != nil {
		t.Fatalf("MoveTask(alpha->dev) = %v", err)
	}

	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{BucketKeys: []string{"dev"}})
	if err != nil {
		t.Fatalf("ListTasks() = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "alpha" {
		t.Fatalf("got %+v, want [alpha]", tasks)
	}
}

func TestListTasksReturnsCreatedAt(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithProject(ctx, t)

	if _, err := store.CreateTask(ctx, project.ID, "task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len = %d, want 1", len(tasks))
	}
	if tasks[0].CreatedAt == "" {
		t.Fatalf("CreatedAt is empty; the schema default should populate it")
	}
}

func openStoreWithProject(ctx context.Context, t *testing.T) (*Store, domain.Project) {
	t.Helper()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Test", "test", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	return store, project
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

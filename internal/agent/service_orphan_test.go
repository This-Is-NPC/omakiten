package agent

import (
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestMigrateOrphans_NoOpReturnsEmptyReport(t *testing.T) {
	fx := newAgentFixture(t)

	resp, err := fx.service.MigrateOrphans(fx.ctx, MigrateOrphansInput{
		ProjectSelector: ProjectSelector{ProjectID: fx.projectA.ID},
	})
	if err != nil {
		t.Fatalf("MigrateOrphans: %v", err)
	}
	if resp.Applied {
		t.Fatal("Applied should be false when there are no orphans")
	}
	if resp.Report.Total != 0 {
		t.Fatalf("Report.Total = %d, want 0", resp.Report.Total)
	}
	if resp.Confirmation.RequiresConfirmation {
		t.Fatal("Confirmation should not be required when report is empty")
	}
}

func TestMigrateOrphans_RequiresConfirmationOnFirstCall(t *testing.T) {
	fx := newAgentFixture(t)

	devTask, err := fx.store.CreateTask(fx.ctx, fx.projectA.ID, "dev task", "", domain.Priority(2), "dev", nil, fx.store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	removeDevBucket(t, fx)

	resp, err := fx.service.MigrateOrphans(fx.ctx, MigrateOrphansInput{
		ProjectSelector: ProjectSelector{ProjectID: fx.projectA.ID},
	})
	if err != nil {
		t.Fatalf("MigrateOrphans: %v", err)
	}
	if resp.Applied {
		t.Fatal("Applied should be false on preview call")
	}
	if !resp.Confirmation.RequiresConfirmation {
		t.Fatal("Confirmation must be required when orphans exist")
	}
	if resp.Report.Total == 0 {
		t.Fatalf("Report.Total = 0; expected orphans; report=%+v", resp.Report)
	}

	tasks, err := fx.store.ListTasks(fx.ctx, fx.projectA.ID, domain.TaskFilter{}, fx.store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, task := range tasks {
		if task.ID == devTask.ID && task.BucketKey == "backlog" {
			t.Fatalf("preview must not mutate: dev task already in backlog")
		}
	}
}

func TestMigrateOrphans_ConfirmedAppliesAndReports(t *testing.T) {
	fx := newAgentFixture(t)

	if _, err := fx.store.CreateTask(fx.ctx, fx.projectA.ID, "dev task", "", domain.Priority(2), "dev", nil, fx.store.Snapshot()); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	removeDevBucket(t, fx)

	resp, err := fx.service.MigrateOrphans(fx.ctx, MigrateOrphansInput{
		ProjectSelector: ProjectSelector{ProjectID: fx.projectA.ID},
		Confirmed:       true,
	})
	if err != nil {
		t.Fatalf("MigrateOrphans confirmed: %v", err)
	}
	if !resp.Applied {
		t.Fatal("Applied should be true after confirmed=true")
	}
	if resp.Report.Total == 0 {
		t.Fatalf("Report.Total = 0; expected migration; report=%+v", resp.Report)
	}

	tasks, err := fx.store.ListTasks(fx.ctx, fx.projectA.ID, domain.TaskFilter{}, fx.store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	migrated := false
	for _, task := range tasks {
		if task.Title == "dev task" && task.BucketKey == "backlog" {
			migrated = true
		}
	}
	if !migrated {
		t.Fatalf("dev task not migrated to backlog; tasks=%+v", tasks)
	}
}

// removeDevBucket re-imports the agent fixture's bundle minus the "dev" bucket
// so any task pointing to dev becomes an orphan. Reuses local_ids 1 and 3 to
// avoid the UNIQUE(workflow_id, key) collision the importer would otherwise hit.
// The agent service's snapshot pointer + injected orphan service are rotated
// to mirror the Store — production wires this through agentruntime.BundleCache;
// tests stitch the rotation by hand because they drive Service.SetSnapshot /
// Service.SetOrphanService directly.
func removeDevBucket(t *testing.T, fx agentFixture) {
	t.Helper()
	previous := fx.store.Snapshot()
	bundle := agentTestBundle(t)
	wf := bundle.Workflows[0]
	wf.Buckets = []config.Bucket{
		{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
		{ID: 3, Key: "done", Name: "Done", Position: 2},
	}
	wf.Transitions = nil
	bundle.Workflows = []config.Workflow{wf}
	if err := fx.store.ImportBundle(fx.ctx, bundle, "test.yaml", "h2"); err != nil {
		t.Fatalf("ImportBundle(remove dev): %v", err)
	}
	current := fx.store.Snapshot()
	fx.service.SetSnapshot(current)
	fx.service.SetOrphanService(app.NewOrphanService(fx.store, current, previous))
}

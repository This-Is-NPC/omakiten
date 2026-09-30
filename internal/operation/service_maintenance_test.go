package operation

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

type recordingMaintenance struct {
	calls []string
}

func (m *recordingMaintenance) BackupDatabase(context.Context, contract.DatabaseBackupInput) (contract.DatabaseBackupResult, error) {
	m.calls = append(m.calls, "backup")
	return contract.DatabaseBackupResult{Path: "/backups/one.db"}, nil
}

func (m *recordingMaintenance) ReindexSearch(context.Context, contract.SearchReindexInput) (contract.SearchReindexResult, error) {
	m.calls = append(m.calls, "reindex")
	return contract.SearchReindexResult{}, nil
}

func (m *recordingMaintenance) DeleteProject(_ context.Context, input contract.ProjectDeleteInput) (contract.ProjectDeleteResult, error) {
	m.calls = append(m.calls, "delete")
	return contract.ProjectDeleteResult{Project: domain.Project{ID: input.ProjectID}}, nil
}

func TestMaintenanceOperationsShipDisabledOnHTTP(t *testing.T) {
	maintenance := &recordingMaintenance{}
	svc := NewService(nil, contract.ProjectSelector{}).ForHTTP()
	svc.SetSnapshot(snapshotWithSurfaces(t, config.CanonicalSurfaceTable()))
	svc.SetMaintenance(maintenance)
	ctx := context.Background()

	_, backupErr := svc.BackupDatabase(ctx, contract.DatabaseBackupInput{})
	_, reindexErr := svc.ReindexSearch(ctx, contract.SearchReindexInput{Confirm: true})
	_, deleteErr := svc.DeleteProject(ctx, contract.ProjectDeleteInput{ProjectID: 1})
	for op, err := range map[string]error{"db.backup": backupErr, "db.reindex": reindexErr, "project.delete": deleteErr} {
		var denied OperationDenied
		if !errors.As(err, &denied) || denied.Op != op || denied.Surface != SurfaceHTTP {
			t.Fatalf("%s err = %v, want OperationDenied on http", op, err)
		}
		if denied.Reason != config.DeniedDestructiveHTTPReason {
			t.Fatalf("%s reason = %q, want %q", op, denied.Reason, config.DeniedDestructiveHTTPReason)
		}
	}
	if len(maintenance.calls) != 0 {
		t.Fatalf("maintenance calls = %v, want none", maintenance.calls)
	}
}

func TestMaintenanceOperationsDelegateOnCLI(t *testing.T) {
	maintenance := &recordingMaintenance{}
	svc := NewService(nil, contract.ProjectSelector{}).ForCLI()
	svc.SetSnapshot(snapshotWithSurfaces(t, config.CanonicalSurfaceTable()))
	svc.SetMaintenance(maintenance)
	ctx := context.Background()

	if _, err := svc.BackupDatabase(ctx, contract.DatabaseBackupInput{}); err != nil {
		t.Fatalf("BackupDatabase: %v", err)
	}
	if _, err := svc.ReindexSearch(ctx, contract.SearchReindexInput{}); err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	if _, err := svc.DeleteProject(ctx, contract.ProjectDeleteInput{ProjectID: 0}); err == nil {
		t.Fatal("DeleteProject(0) error = nil, want validation")
	}
	deleted, err := svc.DeleteProject(ctx, contract.ProjectDeleteInput{ProjectID: 7})
	if err != nil || deleted.Project.ID != 7 {
		t.Fatalf("DeleteProject = %+v, %v", deleted, err)
	}
	if got, want := len(maintenance.calls), 3; got != want {
		t.Fatalf("maintenance calls = %v, want backup, reindex, delete", maintenance.calls)
	}
}

func TestMaintenanceOperationsRequirePort(t *testing.T) {
	svc := NewService(nil, contract.ProjectSelector{}).ForCLI()
	_, err := svc.BackupDatabase(context.Background(), contract.DatabaseBackupInput{})
	assertCodedError(t, err, domain.ErrValidation)
}

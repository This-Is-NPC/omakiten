package operation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func TestRelocateProjectPreservesWorkAfterDirectoryMove(t *testing.T) {
	f := newAgentFixture(t)
	if _, err := f.store.UpdateProjectDescription(f.ctx, f.projectA.ID, "Preserved description"); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(f.projectA.RootPath, root); err != nil {
		t.Fatal(err)
	}
	input := contract.RelocateProjectInput{ProjectSelector: contract.ProjectSelector{Project: f.projectA.Slug}, RootPath: root}
	for i := 0; i < 2; i++ {
		result, err := f.service.RelocateProject(f.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.Project.ID != f.projectA.ID || result.Project.Slug != f.projectA.Slug || result.Project.RootPath != root {
			t.Fatalf("relocated identity: %+v", result)
		}
	}
	project, err := f.store.FindProjectByID(f.ctx, f.projectA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != f.projectA.Name || project.Description != "Preserved description" {
		t.Fatal("project metadata changed")
	}
	continued, err := f.service.ContinueTask(f.ctx, contract.ContinueTaskInput{ProjectSelector: input.ProjectSelector, TaskID: f.taskA1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if continued.Task.ID != f.taskA1.ID || len(continued.Comments) != 1 {
		t.Fatalf("work changed: %+v", continued)
	}
	assertProjectRelocationAudit(t, f, root)
}

func assertProjectRelocationAudit(t *testing.T, f agentFixture, root string) {
	t.Helper()
	rows, err := f.store.ListEvents(f.ctx, domain.EventFilter{ProjectID: f.projectA.ID})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.EventType != domain.EventTypeProjectUpdated {
			continue
		}
		count++
		var payload struct {
			RootPath struct{ From, To string } `json:"root_path"`
		}
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.RootPath.From != f.projectA.RootPath || payload.RootPath.To != root {
			t.Fatalf("audit: %s", row.Payload)
		}
	}
	if count != 1 {
		t.Fatalf("audit count = %d, want 1", count)
	}
}

func TestRelocateProjectRejectsInvalidTargets(t *testing.T) {
	f := newAgentFixture(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"", "relative", filepath.Join(t.TempDir(), "missing"), file} {
		_, err := f.service.RelocateProject(f.ctx, contract.RelocateProjectInput{ProjectSelector: contract.ProjectSelector{ProjectID: f.projectA.ID}, RootPath: root})
		assertCodedError(t, err, domain.ErrValidation)
	}
	_, err := f.service.RelocateProject(f.ctx, contract.RelocateProjectInput{RootPath: t.TempDir()})
	assertCodedError(t, err, domain.ErrValidation)
	_, err = f.service.RelocateProject(f.ctx, contract.RelocateProjectInput{ProjectSelector: contract.ProjectSelector{ProjectID: f.projectA.ID}, RootPath: f.projectB.RootPath})
	assertCodedError(t, err, domain.ErrProjectPathConflict)
	unchanged, err := f.store.FindProjectByID(f.ctx, f.projectA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.RootPath != f.projectA.RootPath {
		t.Fatal("failed relocation changed root")
	}
}

func TestRelocateProjectRejectsSymlink(t *testing.T) {
	f := newAgentFixture(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skip(err)
	}
	_, err := f.service.RelocateProject(f.ctx, contract.RelocateProjectInput{ProjectSelector: contract.ProjectSelector{ProjectID: f.projectA.ID}, RootPath: link})
	assertCodedError(t, err, domain.ErrValidation)
}

func TestRelocateProjectHonorsSurfacePermissions(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	denied := false
	row := table["project.relocate"]
	row.CLI, row.HTTP = &denied, &denied
	table["project.relocate"] = row
	svc := NewService(nil, contract.ProjectSelector{})
	svc.SetSnapshot(snapshotWithSurfaces(t, table))
	for _, facade := range []*Service{svc.ForCLI(), svc.ForHTTP()} {
		_, err := facade.RelocateProject(context.Background(), contract.RelocateProjectInput{})
		var rejection OperationDenied
		if !errors.As(err, &rejection) || rejection.Op != "project.relocate" {
			t.Fatalf("permission rejection: %v", err)
		}
	}
}

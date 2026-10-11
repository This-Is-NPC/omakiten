package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"

	"omakiten/internal/domain"
)

func TestProjectRelocationRejectsStaleRoot(t *testing.T) {
	ctx := context.Background()
	s := openStoreFixture(t, t.TempDir()+"/db")
	p, err := s.UpsertProject(ctx, "Project", "project", "/old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProjectRootPath(ctx, p.ID, "/old", "/new"); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"/third", "/old"} {
		_, err := s.UpdateProjectRootPath(ctx, p.ID, "/old", root)
		var coded *domain.CodedError
		if !errors.As(err, &coded) || coded.Code != domain.ErrProjectPathConflict {
			t.Fatalf("stale update: %v", err)
		}
	}
	got, err := s.FindProjectByID(ctx, p.ID)
	if err != nil || got.RootPath != "/new" {
		t.Fatalf("root after stale update: %+v, %v", got, err)
	}
}

func TestProjectRelocationRollsBackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	s := openStoreFixture(t, t.TempDir()+"/db")
	p, err := s.UpsertProject(ctx, "Project", "project", "/old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_root_audit BEFORE INSERT ON events WHEN NEW.event_type = 'project.updated' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProjectRootPath(ctx, p.ID, "/old", "/new"); err == nil {
		t.Fatal("expected failed audit")
	}
	got, err := s.FindProjectByID(ctx, p.ID)
	if err != nil || got.RootPath != "/old" {
		t.Fatalf("root after rollback: %+v, %v", got, err)
	}
}

func TestProjectRelocationSerializesConcurrentCorrections(t *testing.T) {
	ctx := context.Background()
	s := openStoreFixture(t, t.TempDir()+"/db")
	p, err := s.UpsertProject(ctx, "Project", "project", "/old")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, root := range []string{"/new-a", "/new-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.UpdateProjectRootPath(ctx, p.ID, "/old", root)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var coded *domain.CodedError
		if !errors.As(err, &coded) || coded.Code != domain.ErrProjectPathConflict {
			t.Fatalf("concurrent update: %v", err)
		}
		conflicts++
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	rows, err := s.ListEvents(ctx, domain.EventFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.EventType == domain.EventTypeProjectUpdated {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("concurrent update audit count: %d", count)
	}
}

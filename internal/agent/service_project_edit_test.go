package agent

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestEditProjectPersistsAndEmitsProjectUpdated locks the restored
// description write path: EditProject must persist through the store's
// UpdateProjectDescription method and emit a project.updated audit
// event keyed by the project id with a description from→to payload.
func TestEditProjectPersistsAndEmitsProjectUpdated(t *testing.T) {
	fixture := newAgentFixture(t)

	const want = "A backlog management tool for AI-driven workflows."
	resp, err := fixture.service.EditProject(fixture.ctx, EditProjectInput{
		ProjectSelector: ProjectSelector{ProjectID: fixture.projectA.ID},
		Description:     want,
	})
	if err != nil {
		t.Fatalf("EditProject() error = %v", err)
	}
	if resp.Project.ID != fixture.projectA.ID {
		t.Fatalf("EditProject().Project.ID = %d, want %d", resp.Project.ID, fixture.projectA.ID)
	}
	if resp.Description != want {
		t.Fatalf("EditProject().Description = %q, want %q", resp.Description, want)
	}

	// Persisted: a fresh read of the row through the store returns the
	// new description (proves UpdateProjectDescription committed, not
	// just that the DTO echoed the input).
	persisted, err := fixture.store.FindProjectByID(fixture.ctx, fixture.projectA.ID)
	if err != nil {
		t.Fatalf("FindProjectByID() error = %v", err)
	}
	if persisted.Description != want {
		t.Fatalf("persisted Description = %q, want %q", persisted.Description, want)
	}

	// Emitted: a project.updated event landed for the project carrying
	// the description from→to delta.
	rows, err := fixture.store.ListEvents(fixture.ctx, domain.EventFilter{ProjectID: fixture.projectA.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var found *domain.EventRow
	for i := range rows {
		if rows[i].EventType == domain.EventTypeProjectUpdated {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no %s event emitted; rows = %#v", domain.EventTypeProjectUpdated, rows)
	}
	if found.EntityType != domain.EventEntityProject {
		t.Fatalf("%s entity_type = %q, want %q", domain.EventTypeProjectUpdated, found.EntityType, domain.EventEntityProject)
	}
	if found.EntityID != fixture.projectA.ID {
		t.Fatalf("%s entity_id = %d, want %d", domain.EventTypeProjectUpdated, found.EntityID, fixture.projectA.ID)
	}
	if !strings.Contains(found.Payload, `"to":"`+want+`"`) {
		t.Fatalf("%s payload = %q, want a description.to of %q", domain.EventTypeProjectUpdated, found.Payload, want)
	}
}

// TestEditProjectNoChangeSkipsEvent locks the no-op contract: editing a
// project to its current description neither errors nor emits a
// project.updated event (the description column starts empty on a fresh
// project, so writing "" is a no-op).
func TestEditProjectNoChangeSkipsEvent(t *testing.T) {
	fixture := newAgentFixture(t)

	if _, err := fixture.service.EditProject(fixture.ctx, EditProjectInput{
		ProjectSelector: ProjectSelector{ProjectID: fixture.projectA.ID},
		Description:     "",
	}); err != nil {
		t.Fatalf("EditProject() error = %v", err)
	}

	rows, err := fixture.store.ListEvents(fixture.ctx, domain.EventFilter{ProjectID: fixture.projectA.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, row := range rows {
		if row.EventType == domain.EventTypeProjectUpdated {
			t.Fatalf("unexpected %s event emitted for a no-op edit", domain.EventTypeProjectUpdated)
		}
	}
}

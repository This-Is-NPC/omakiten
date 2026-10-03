package operation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
)

type emitFixture struct {
	ctx     context.Context
	store   *snapstore.Store
	service *Service
	project domain.Project
}

// newEmitFixture declares external.ci_failed in the default kit.
func newEmitFixture(t *testing.T) emitFixture {
	t.Helper()
	ctx := context.Background()
	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	project := "project"
	bundle.Config.Events.Definitions[domain.EventTypeExternalPrefix+"ci_failed"] = config.EventDefinitionSettings{
		Category: string(domain.EventCategoryExternal), Display: "CI failed", EntityType: &project, Formatter: string(domain.ExternalFormatter),
	}
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	// The runtime hands the store the kit's events, as the composition root does.
	if err := store.ApplyConfig(ctx, sqlite.ConfigKnobs{EventsPolicy: bundle.Config.Events, EventsDefaultRecentLimit: bundle.Config.Events.DefaultRecentLimit}); err != nil {
		t.Fatalf("ApplyConfig() error = %v", err)
	}
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	created, err := store.UpsertProject(ctx, "Project", "project", root)
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	svc := NewService(store, contract.ProjectSelector{CWD: root})
	svc.SetSnapshot(store.Snapshot())
	return emitFixture{ctx: ctx, store: store, service: svc, project: created}
}

func (f emitFixture) emit(name string, payload map[string]string) (contract.EmitEventResponse, error) {
	return f.service.EmitEvent(f.ctx, contract.EmitEventInput{Name: name, Payload: payload})
}

func TestEmitEventRecordsTheDeclaredEvent(t *testing.T) {
	f := newEmitFixture(t)
	resp, err := f.emit("ci_failed", map[string]string{"branch": "main"})
	if err != nil {
		t.Fatalf("EmitEvent() error = %v", err)
	}
	if resp.EventType != "external.ci_failed" || resp.Project.ID != f.project.ID {
		t.Fatalf("EmitEvent() = %+v, want external.ci_failed on project %d", resp, f.project.ID)
	}
	rows, err := f.store.ListEvents(f.ctx, domain.EventFilter{ProjectID: f.project.ID, Categories: []domain.EventCategory{domain.EventCategoryExternal}})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("external rows = %d, want 1", len(rows))
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil || payload["branch"] != "main" {
		t.Fatalf("payload = %q (%v), want branch=main", rows[0].Payload, err)
	}
	if rows[0].EntityType != domain.EventEntityProject || rows[0].EntityID != f.project.ID {
		t.Fatalf("entity = %s#%d, want project#%d", rows[0].EntityType, rows[0].EntityID, f.project.ID)
	}
}

func TestEmitEventRejectsWhatTheKitDoesNotDeclare(t *testing.T) {
	f := newEmitFixture(t)
	cases := map[string]struct {
		name    string
		payload map[string]string
	}{
		"undeclared name": {"deploy_done", nil},
		"malformed name":  {"CI-Failed", nil},
		"internal event":  {"../task.created", nil},
		"malformed field": {"ci_failed", map[string]string{"Branch": "main"}},
		"oversized value": {"ci_failed", map[string]string{"log": strings.Repeat("x", domain.MaxExternalValueBytes+1)}},
		"too many fields": {"ci_failed", manyFields(domain.MaxExternalFields + 1)},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := f.emit(tc.name, tc.payload)
			assertCodedError(t, err, domain.ErrValidation)
		})
	}
	rows, err := f.store.ListEvents(f.ctx, domain.EventFilter{ProjectID: f.project.ID, Categories: []domain.EventCategory{domain.EventCategoryExternal}})
	if err != nil || len(rows) != 0 {
		t.Fatalf("external rows = %d (%v), want 0 after rejected emits", len(rows), err)
	}
}

func TestEmitEventLimitsTheRatePerProject(t *testing.T) {
	f := newEmitFixture(t)
	for i := 0; i < domain.MaxExternalEventsPerMinute; i++ {
		if _, err := f.emit("ci_failed", nil); err != nil {
			t.Fatalf("emit %d error = %v", i, err)
		}
	}
	_, err := f.emit("ci_failed", nil)
	assertCodedError(t, err, domain.ErrRateLimited)
}

func manyFields(n int) map[string]string {
	out := make(map[string]string, n)
	for i := 0; i < n; i++ {
		out["f"+strings.Repeat("a", i)] = "v"
	}
	return out
}

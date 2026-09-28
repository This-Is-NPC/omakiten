package operation

import (
	"testing"

	"omakiten/internal/domain"
)

func TestListLawsOmitsBodies(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t, nil,
		[]LawInfo{{Slug: "scope", Name: "Scope", Severity: "error", Body: "stay scoped", Scope: "global"}},
		nil, nil, nil,
	))

	resp, err := fixture.service.ListLaws(fixture.ctx, ListLawsInput{})
	if err != nil {
		t.Fatalf("ListLaws() error = %v", err)
	}
	if len(resp.Laws) != 1 || resp.Laws[0].Body != "" {
		t.Fatalf("ListLaws() = %+v, want one law without body", resp.Laws)
	}
}

func TestShowLawReturnsBody(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t, nil,
		[]LawInfo{{Slug: "scope", Severity: "error", Body: "stay scoped"}},
		nil, nil, nil,
	))

	resp, err := fixture.service.ShowLaw(fixture.ctx, ShowLawInput{Slug: "scope"})
	if err != nil {
		t.Fatalf("ShowLaw() error = %v", err)
	}
	if resp.Law.Body == "" {
		t.Fatal("ShowLaw missing body")
	}
}

func TestShowLawRejectsUnknownSlug(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t, nil, nil, nil, nil, nil))

	_, err := fixture.service.ShowLaw(fixture.ctx, ShowLawInput{Slug: "ghost"})
	assertCodedError(t, err, domain.ErrValidation)
}

func TestListLawsFilters(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t, nil,
		[]LawInfo{
			{Slug: "global-rule", Name: "Global", Severity: "error", Body: "g", Scope: "global"},
			{Slug: "project-rule", Name: "Project", Severity: "warn", Body: "p", Scope: "project", Project: "alpha"},
			{Slug: "persona-rule", Name: "Persona", Severity: "info", Body: "x", Scope: "persona", Persona: "builder"},
		},
		nil, nil, nil,
	))

	resp, err := fixture.service.ListLaws(fixture.ctx, ListLawsInput{Scope: "project"})
	if err != nil {
		t.Fatalf("ListLaws(scope=project) error = %v", err)
	}
	if len(resp.Laws) != 1 || resp.Laws[0].Slug != "project-rule" || resp.Laws[0].Project != "alpha" {
		t.Fatalf("ListLaws(scope=project) = %+v, want project-rule/alpha", resp.Laws)
	}

	resp, err = fixture.service.ListLaws(fixture.ctx, ListLawsInput{Persona: "builder"})
	if err != nil {
		t.Fatalf("ListLaws(persona=builder) error = %v", err)
	}
	if len(resp.Laws) != 1 || resp.Laws[0].Slug != "persona-rule" {
		t.Fatalf("ListLaws(persona=builder) = %+v, want persona-rule", resp.Laws)
	}

	resp, err = fixture.service.ListLaws(fixture.ctx, ListLawsInput{Project: "missing"})
	if err != nil {
		t.Fatalf("ListLaws(project=missing) error = %v", err)
	}
	if len(resp.Laws) != 0 {
		t.Fatalf("ListLaws(project=missing) = %+v, want empty", resp.Laws)
	}
}

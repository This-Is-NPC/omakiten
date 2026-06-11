package agent

import (
	"testing"

	"omakiten/internal/domain"
)

func TestListPersonasOmitsBodies(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t,
		[]SkillInfo{{Slug: "go", Name: "Go", Body: "go body"}},
		[]LawInfo{{Slug: "scope", Severity: "error", Body: "scope body"}},
		[]PersonaInfo{{
			Slug:        "builder",
			Name:        "Builder",
			Description: "Builds things.",
			Body:        "You are a builder.",
			Laws:        []string{"scope"},
			Skills:      []string{"go"},
		}},
		nil, nil,
	))

	resp, err := fixture.service.ListPersonas(fixture.ctx, ListPersonasInput{})
	if err != nil {
		t.Fatalf("ListPersonas() error = %v", err)
	}
	if len(resp.Personas) != 1 {
		t.Fatalf("ListPersonas() = %+v, want one persona", resp.Personas)
	}
	p := resp.Personas[0]
	if p.Slug != "builder" || p.Name != "Builder" || p.Description != "Builds things." {
		t.Fatalf("ListPersonas summary = %+v, want identity fields only", p)
	}
}

func TestShowPersonaExpandsReferences(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t,
		[]SkillInfo{{Slug: "go", Name: "Go", Description: "Go lang", Body: "go body"}},
		[]LawInfo{{Slug: "scope", Name: "Scope", Severity: "error", Body: "scope body"}},
		[]PersonaInfo{{
			Slug:            "builder",
			Name:            "Builder",
			Body:            "You are a builder.",
			Laws:            []string{"scope"},
			SkillRepertoire: []string{"go"},
		}},
		nil, nil,
	))

	resp, err := fixture.service.ShowPersona(fixture.ctx, ShowPersonaInput{Slug: "builder"})
	if err != nil {
		t.Fatalf("ShowPersona() error = %v", err)
	}
	if resp.Persona.Body == "" {
		t.Fatal("ShowPersona missing body")
	}
	if len(resp.Persona.Laws) != 1 || resp.Persona.Laws[0].Body == "" {
		t.Fatalf("ShowPersona laws = %+v, want expanded law body", resp.Persona.Laws)
	}
	if len(resp.Persona.SkillRepertoire) != 1 || resp.Persona.SkillRepertoire[0].Body == "" {
		t.Fatalf("ShowPersona skill_repertoire = %+v, want expanded skill body", resp.Persona.SkillRepertoire)
	}
}

func TestShowPersonaRejectsUnknownSlug(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t, nil, nil, nil, nil, nil))

	_, err := fixture.service.ShowPersona(fixture.ctx, ShowPersonaInput{Slug: "missing"})
	assertCodedError(t, err, domain.ErrValidation)
}

func TestShowPersonaRejectsBrokenLawRef(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetSnapshot(snapshotWithEntities(t,
		nil,
		[]LawInfo{{Slug: "scope", Severity: "error", Body: "scope"}},
		[]PersonaInfo{{Slug: "builder", Laws: []string{"ghost-law"}}},
		nil, nil,
	))

	_, err := fixture.service.ShowPersona(fixture.ctx, ShowPersonaInput{Slug: "builder"})
	assertCodedError(t, err, domain.ErrValidation)
}

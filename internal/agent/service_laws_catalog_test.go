package agent

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

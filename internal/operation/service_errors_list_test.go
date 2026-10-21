package operation

import (
	"testing"

	"omakiten/internal/contract"
)

// TestListErrorsFiltersByTagNewestFirst verifies error.list returns the
// project's errors newest first with their tags, and that --tag keeps only
// errors carrying the normalised tag.
func TestListErrorsFiltersByTagNewestFirst(t *testing.T) {
	fixture := newAgentFixture(t)
	record := func(description string, tags ...string) int64 {
		t.Helper()
		resp, err := fixture.service.RecordError(fixture.ctx, contract.RecordErrorInput{Description: description, Tags: tags})
		if err != nil {
			t.Fatalf("RecordError(%q) error = %v", description, err)
		}
		return resp.Error.ID
	}
	first := record("wrong branch", "agent-correction", "git")
	record("flaky test", "ci")
	third := record("skipped review", "agent-correction")

	all, err := fixture.service.ListErrors(fixture.ctx, contract.ListErrorsInput{})
	if err != nil {
		t.Fatalf("ListErrors() error = %v", err)
	}
	if len(all.Errors) != 3 || all.Errors[0].ID != third {
		t.Fatalf("ListErrors() = %+v, want 3 errors newest first", all.Errors)
	}

	tagged, err := fixture.service.ListErrors(fixture.ctx, contract.ListErrorsInput{Tag: "Agent Correction"})
	if err != nil {
		t.Fatalf("ListErrors(tag) error = %v", err)
	}
	if len(tagged.Errors) != 2 || tagged.Errors[0].ID != third || tagged.Errors[1].ID != first {
		t.Fatalf("ListErrors(tag) = %+v, want errors %d and %d", tagged.Errors, third, first)
	}
	if got := len(tagged.Errors[1].Tags); got != 2 {
		t.Fatalf("error %d carries %d tags, want 2 (agent-correction, git)", first, got)
	}
}

package operation

import (
	"testing"
	"time"

	"omakiten/internal/contract"
)

// TestWaitReturnsTheFirstMatchingComment verifies wait ignores comments
// written before it started and returns once a comment with a matching tag
// lands, and times out otherwise.
func TestWaitReturnsTheFirstMatchingComment(t *testing.T) {
	fixture := newAgentFixture(t)
	add := func(tag string) {
		t.Helper()
		if _, err := fixture.service.AddComment(fixture.ctx, contract.AddCommentInput{TaskID: fixture.taskA1.ID, Body: "result", AuthorType: "agent", Tags: []string{tag}}); err != nil {
			t.Fatalf("AddComment(%s) error = %v", tag, err)
		}
	}
	add("review-security")

	timed, err := fixture.service.Wait(fixture.ctx, contract.WaitInput{TaskID: fixture.taskA1.ID, Tag: "review-*", Timeout: "300ms"})
	if err != nil || !timed.TimedOut || len(timed.Matched) != 0 {
		t.Fatalf("Wait() over an old comment = %+v, %v; want a timeout", timed, err)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		add("to-lead")
		add("review-perf")
	}()
	got, err := fixture.service.Wait(fixture.ctx, contract.WaitInput{TaskID: fixture.taskA1.ID, Tag: "review-*", Timeout: "5s", AfterID: timed.AfterID})
	if err != nil || got.TimedOut || len(got.Matched) != 1 || got.Matched[0].Tags[0].Name != "review-perf" {
		t.Fatalf("Wait() = %+v, %v; want the review-perf comment", got, err)
	}
	if got.AfterID != got.Matched[0].ID {
		t.Fatalf("AfterID = %d, want the matched comment id %d", got.AfterID, got.Matched[0].ID)
	}
}

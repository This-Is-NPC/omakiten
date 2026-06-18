package agent

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestCreateTaskInputCapsSmoke is the MCP-boundary smoke test for the
// domain length caps: a title or description of len == cap+1 submitted
// through Service.CreateTask is rejected with ErrValidation, confirming the
// domain cap is inherited at the MCP trust boundary (not only the CLI/service
// path). len == cap is accepted.
func TestCreateTaskInputCapsSmoke(t *testing.T) {
	f := newAgentFixture(t)

	t.Run("title at cap accepted", func(t *testing.T) {
		out, err := f.service.CreateTask(f.ctx, CreateTaskInput{
			Title:     strings.Repeat("t", domain.MaxTaskTitleRunes),
			BucketKey: "backlog",
		})
		if err != nil {
			t.Fatalf("CreateTask(title len==cap) error = %v, want nil", err)
		}
		if out.Task == nil {
			t.Fatal("CreateTask(title len==cap) returned no task")
		}
	})

	t.Run("title over cap rejected", func(t *testing.T) {
		_, err := f.service.CreateTask(f.ctx, CreateTaskInput{
			Title:     strings.Repeat("t", domain.MaxTaskTitleRunes+1),
			BucketKey: "backlog",
		})
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("description over cap rejected", func(t *testing.T) {
		_, err := f.service.CreateTask(f.ctx, CreateTaskInput{
			Title:       "ok",
			Description: strings.Repeat("d", domain.MaxTaskDescriptionBytes+1),
			BucketKey:   "backlog",
		})
		assertCodedError(t, err, domain.ErrValidation)
	})
}

// TestAddCommentInputCapSmoke is the MCP-boundary smoke test for the comment
// body byte cap: a body of len == cap+1 submitted through Service.AddComment
// is rejected with ErrValidation; len == cap is accepted.
func TestAddCommentInputCapSmoke(t *testing.T) {
	f := newAgentFixture(t)

	t.Run("body at cap accepted", func(t *testing.T) {
		_, err := f.service.AddComment(f.ctx, AddCommentInput{
			TaskID:     f.taskA1.ID,
			Body:       strings.Repeat("b", domain.MaxCommentBodyBytes),
			AuthorType: "agent",
		})
		if err != nil {
			t.Fatalf("AddComment(body len==cap) error = %v, want nil", err)
		}
	})

	t.Run("body over cap rejected", func(t *testing.T) {
		_, err := f.service.AddComment(f.ctx, AddCommentInput{
			TaskID:     f.taskA1.ID,
			Body:       strings.Repeat("b", domain.MaxCommentBodyBytes+1),
			AuthorType: "agent",
		})
		assertCodedError(t, err, domain.ErrValidation)
	})
}

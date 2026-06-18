package app

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

// TestTaskServiceInputCaps proves the domain length caps are enforced on
// the task create/edit write paths: len == cap passes, len == cap+1 is
// rejected with ErrValidation (the kind is asserted) and never silently
// truncated.
func TestTaskServiceInputCaps(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	t.Run("title at cap passes", func(t *testing.T) {
		title := strings.Repeat("t", domain.MaxTaskTitleRunes)
		task, err := service.Add(ctx, project.Context(), title, "", "", "backlog")
		if err != nil {
			t.Fatalf("Add(title len==cap) error = %v, want nil", err)
		}
		if task.Title != title {
			t.Fatalf("title at cap was altered; got len %d, want %d (no truncation)", len([]rune(task.Title)), domain.MaxTaskTitleRunes)
		}
	})

	t.Run("title over cap rejects", func(t *testing.T) {
		title := strings.Repeat("t", domain.MaxTaskTitleRunes+1)
		_, err := service.Add(ctx, project.Context(), title, "", "", "backlog")
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("description at cap passes", func(t *testing.T) {
		desc := strings.Repeat("d", domain.MaxTaskDescriptionBytes)
		task, err := service.Add(ctx, project.Context(), "ok", desc, "", "backlog")
		if err != nil {
			t.Fatalf("Add(desc len==cap) error = %v, want nil", err)
		}
		if len(task.Description) != domain.MaxTaskDescriptionBytes {
			t.Fatalf("description at cap was altered; got %d bytes, want %d (no truncation)", len(task.Description), domain.MaxTaskDescriptionBytes)
		}
	})

	t.Run("description over cap rejects", func(t *testing.T) {
		desc := strings.Repeat("d", domain.MaxTaskDescriptionBytes+1)
		_, err := service.Add(ctx, project.Context(), "ok", desc, "", "backlog")
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("edit title over cap rejects", func(t *testing.T) {
		task, err := service.Add(ctx, project.Context(), "editable", "", "", "backlog")
		if err != nil {
			t.Fatalf("Add(seed) error = %v", err)
		}
		over := strings.Repeat("t", domain.MaxTaskTitleRunes+1)
		_, err = service.Edit(ctx, project.Context(), task.ID, domain.TaskUpdate{Title: &over})
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("edit description over cap rejects", func(t *testing.T) {
		task, err := service.Add(ctx, project.Context(), "editable2", "", "", "backlog")
		if err != nil {
			t.Fatalf("Add(seed) error = %v", err)
		}
		over := strings.Repeat("d", domain.MaxTaskDescriptionBytes+1)
		_, err = service.Edit(ctx, project.Context(), task.ID, domain.TaskUpdate{Description: &over})
		assertCodedError(t, err, domain.ErrValidation)
	})
}

// TestCommentServiceInputCap proves the comment body byte cap is enforced
// on the write path (add): len == cap passes, len == cap+1 is rejected with
// ErrValidation rather than truncated.
func TestCommentServiceInputCap(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	taskService := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())
	task, err := taskService.Add(ctx, project.Context(), "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(task) error = %v", err)
	}

	service := NewCommentService(store, store.Snapshot())

	t.Run("body at cap passes", func(t *testing.T) {
		body := strings.Repeat("b", domain.MaxCommentBodyBytes)
		comment, err := service.Add(ctx, project.Context(), task.ID, body, "human", nil)
		if err != nil {
			t.Fatalf("Add(body len==cap) error = %v, want nil", err)
		}
		if len(comment.Body) != domain.MaxCommentBodyBytes {
			t.Fatalf("comment body at cap was altered; got %d bytes, want %d (no truncation)", len(comment.Body), domain.MaxCommentBodyBytes)
		}
	})

	t.Run("body over cap rejects", func(t *testing.T) {
		body := strings.Repeat("b", domain.MaxCommentBodyBytes+1)
		_, err := service.Add(ctx, project.Context(), task.ID, body, "human", nil)
		assertCodedError(t, err, domain.ErrValidation)
	})
}

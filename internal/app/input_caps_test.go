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

// TestPlanServiceGoalBodyInputCap proves the plan goal_body byte cap is
// enforced through the app service on both create and update. len == cap is
// stored unaltered; len == cap+1 is rejected with ErrValidation.
func TestPlanServiceGoalBodyInputCap(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	service := NewPlanService(store)

	t.Run("create goal_body at cap passes", func(t *testing.T) {
		body := strings.Repeat("g", domain.MaxPlanGoalBodyBytes)
		plan, err := service.Create(ctx, project.Context(), "goal-at-cap", "Goal at cap", body)
		if err != nil {
			t.Fatalf("Create(goal_body len==cap) error = %v, want nil", err)
		}
		if plan.GoalBody != body {
			t.Fatalf("goal_body at cap was altered; got %d bytes, want %d (no truncation)", len(plan.GoalBody), domain.MaxPlanGoalBodyBytes)
		}
	})

	t.Run("create goal_body over cap rejects", func(t *testing.T) {
		body := strings.Repeat("g", domain.MaxPlanGoalBodyBytes+1)
		_, err := service.Create(ctx, project.Context(), "goal-over-cap", "Goal over cap", body)
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("update goal_body at cap passes", func(t *testing.T) {
		plan, err := service.Create(ctx, project.Context(), "goal-edit-at-cap", "Goal edit", "old")
		if err != nil {
			t.Fatalf("Create(seed) error = %v", err)
		}
		body := strings.Repeat("u", domain.MaxPlanGoalBodyBytes)
		updated, err := service.UpdateGoalBody(ctx, project.Context(), plan.ID, body)
		if err != nil {
			t.Fatalf("UpdateGoalBody(goal_body len==cap) error = %v, want nil", err)
		}
		if updated.GoalBody != body {
			t.Fatalf("updated goal_body at cap was altered; got %d bytes, want %d (no truncation)", len(updated.GoalBody), domain.MaxPlanGoalBodyBytes)
		}
	})

	t.Run("update goal_body over cap rejects", func(t *testing.T) {
		plan, err := service.Create(ctx, project.Context(), "goal-edit-over-cap", "Goal edit", "old")
		if err != nil {
			t.Fatalf("Create(seed) error = %v", err)
		}
		body := strings.Repeat("u", domain.MaxPlanGoalBodyBytes+1)
		_, err = service.UpdateGoalBody(ctx, project.Context(), plan.ID, body)
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

// TestCommentServiceScopedMetadataInputCaps proves the comment note metadata
// caps are enforced on scoped create/edit paths: len == cap is stored
// unaltered, while len == cap+1 returns ErrValidation.
func TestCommentServiceScopedMetadataInputCaps(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	service := NewCommentService(store, store.Snapshot())
	seed := func(t *testing.T) domain.Comment {
		t.Helper()
		comment, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
			Scope:      domain.CommentScopeProject,
			Body:       "seed",
			AuthorType: "agent",
		})
		if err != nil {
			t.Fatalf("AddScoped(seed) error = %v", err)
		}
		return comment
	}

	t.Run("create title at cap passes", func(t *testing.T) {
		title := strings.Repeat("t", domain.MaxCommentTitleRunes)
		comment, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
			Scope:      domain.CommentScopeProject,
			Body:       "body",
			Title:      title,
			AuthorType: "agent",
		})
		if err != nil {
			t.Fatalf("AddScoped(title len==cap) error = %v, want nil", err)
		}
		if comment.Title != title {
			t.Fatalf("comment title at cap was altered; got %d runes, want %d (no truncation)", len([]rune(comment.Title)), domain.MaxCommentTitleRunes)
		}
	})

	t.Run("create title over cap rejects", func(t *testing.T) {
		title := strings.Repeat("t", domain.MaxCommentTitleRunes+1)
		_, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
			Scope:      domain.CommentScopeProject,
			Body:       "body",
			Title:      title,
			AuthorType: "agent",
		})
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("edit title at cap passes", func(t *testing.T) {
		comment := seed(t)
		title := strings.Repeat("e", domain.MaxCommentTitleRunes)
		edited, err := service.EditScoped(ctx, project.Context(), comment.ID, domain.CommentEdit{Title: &title}, nil)
		if err != nil {
			t.Fatalf("EditScoped(title len==cap) error = %v, want nil", err)
		}
		if edited.Title != title {
			t.Fatalf("edited title at cap was altered; got %d runes, want %d (no truncation)", len([]rune(edited.Title)), domain.MaxCommentTitleRunes)
		}
	})

	t.Run("edit title over cap rejects", func(t *testing.T) {
		comment := seed(t)
		title := strings.Repeat("e", domain.MaxCommentTitleRunes+1)
		_, err := service.EditScoped(ctx, project.Context(), comment.ID, domain.CommentEdit{Title: &title}, nil)
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("create kind at cap passes", func(t *testing.T) {
		kind := strings.Repeat("k", domain.MaxCommentKindRunes)
		comment, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
			Scope:      domain.CommentScopeProject,
			Body:       "body",
			Kind:       kind,
			AuthorType: "agent",
		})
		if err != nil {
			t.Fatalf("AddScoped(kind len==cap) error = %v, want nil", err)
		}
		if comment.Kind != kind {
			t.Fatalf("comment kind at cap was altered; got %d runes, want %d (no truncation)", len([]rune(comment.Kind)), domain.MaxCommentKindRunes)
		}
	})

	t.Run("create kind over cap rejects", func(t *testing.T) {
		kind := strings.Repeat("k", domain.MaxCommentKindRunes+1)
		_, err := service.AddScoped(ctx, project.Context(), domain.CommentWrite{
			Scope:      domain.CommentScopeProject,
			Body:       "body",
			Kind:       kind,
			AuthorType: "agent",
		})
		assertCodedError(t, err, domain.ErrValidation)
	})

	t.Run("edit kind at cap passes", func(t *testing.T) {
		comment := seed(t)
		kind := strings.Repeat("m", domain.MaxCommentKindRunes)
		edited, err := service.EditScoped(ctx, project.Context(), comment.ID, domain.CommentEdit{Kind: &kind}, nil)
		if err != nil {
			t.Fatalf("EditScoped(kind len==cap) error = %v, want nil", err)
		}
		if edited.Kind != kind {
			t.Fatalf("edited kind at cap was altered; got %d runes, want %d (no truncation)", len([]rune(edited.Kind)), domain.MaxCommentKindRunes)
		}
	})

	t.Run("edit kind over cap rejects", func(t *testing.T) {
		comment := seed(t)
		kind := strings.Repeat("m", domain.MaxCommentKindRunes+1)
		_, err := service.EditScoped(ctx, project.Context(), comment.ID, domain.CommentEdit{Kind: &kind}, nil)
		assertCodedError(t, err, domain.ErrValidation)
	})
}

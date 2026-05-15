package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type CommentService struct {
	repo     CommentRepository
	workflow *WorkflowService
	synonyms map[string]string
}

func NewCommentService(repo CommentRepository) *CommentService {
	return &CommentService{repo: repo}
}

// NewCommentServiceWithWorkflow wires policy enforcement on Edit/Remove. The
// plain NewCommentService stays for read-only callers (Add/List) so existing
// tests don't have to thread a workflow stub everywhere.
func NewCommentServiceWithWorkflow(repo CommentRepository, workflow *WorkflowService) *CommentService {
	return &CommentService{repo: repo, workflow: workflow}
}

// SetSynonyms installs the per-project tag-synonym table the service
// passes to NormalizeTagName when processing comment tags. Phase 3f
// replaced the process-global registry with this per-service field.
func (s *CommentService) SetSynonyms(synonyms map[string]string) {
	s.synonyms = synonyms
}

func (s *CommentService) Add(ctx context.Context, project domain.ProjectContext, taskID int64, body, authorType string, rawTags []string) (comment domain.Comment, err error) {
	finish := activity.Track(ctx, "app.CommentService.Add", project, map[string]any{"task_id": taskID, "author": authorType})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID <= 0 {
		err = domain.NewError(domain.ErrValidation, "task id must be positive", nil)
		return
	}
	body = strings.TrimSpace(body)
	if body == "" {
		err = domain.NewError(domain.ErrValidation, "comment body is required", nil)
		return
	}
	authorType = strings.TrimSpace(authorType)
	if authorType == "" {
		authorType = "human"
	}
	if authorType != "human" && authorType != "agent" {
		err = domain.NewError(domain.ErrValidation, "author type must be human or agent", map[string]any{"author_type": authorType})
		return
	}

	tags := make([]domain.Tag, 0, len(rawTags))
	for _, raw := range rawTags {
		name := NormalizeTagName(raw, s.synonyms)
		if name == "" {
			continue
		}
		tags = append(tags, domain.Tag{Name: name, Label: TagLabel(raw)})
	}

	comment, err = s.repo.AddComment(ctx, project.ID, taskID, body, authorType, tags)
	return
}

// Edit rewrites a comment's body and replaces its tags after enforcing the
// per-bucket comment.edit policy. Inheritance: when permissions.comment is
// missing on the bucket, the comment policy mirrors permissions.task; when
// partially set, only declared fields override.
func (s *CommentService) Edit(ctx context.Context, project domain.ProjectContext, commentID int64, body string, rawTags []string) (comment domain.Comment, err error) {
	finish := activity.Track(ctx, "app.CommentService.Edit", project, map[string]any{"comment_id": commentID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if commentID <= 0 {
		err = domain.NewError(domain.ErrValidation, "comment id must be positive", nil)
		return
	}
	body = strings.TrimSpace(body)
	if body == "" {
		err = domain.NewError(domain.ErrValidation, "comment body is required", nil)
		return
	}

	existing, err := s.repo.CommentByID(ctx, project.ID, commentID)
	if err != nil {
		return
	}

	if s.workflow != nil {
		var allowed bool
		var hint string
		allowed, hint, err = s.workflow.ResolveBucketPermissions(ctx, project, existing.TaskID, EntityComment, PermissionEdit)
		if err != nil {
			return
		}
		if !allowed {
			s.workflow.EmitGuardViolated(ctx, project.ID, domain.EventEntityTask, existing.TaskID,
				GuardOperationCommentEdit, GuardRulePermissions, hint,
				map[string]any{"comment_id": commentID, "task_id": existing.TaskID, "entity": EntityComment, "operation": PermissionEdit})
			err = domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"comment_id": commentID, "task_id": existing.TaskID, "hint": hint, "entity": EntityComment, "operation": PermissionEdit})
			return
		}
	}

	tags := make([]domain.Tag, 0, len(rawTags))
	for _, raw := range rawTags {
		name := NormalizeTagName(raw, s.synonyms)
		if name == "" {
			continue
		}
		tags = append(tags, domain.Tag{Name: name, Label: TagLabel(raw)})
	}

	comment, _, err = s.repo.UpdateComment(ctx, project.ID, commentID, body, tags)
	return
}

// Remove hard-deletes a comment after enforcing the per-bucket comment.delete
// policy. Emits comment.removed with the body snapshot for audit.
func (s *CommentService) Remove(ctx context.Context, project domain.ProjectContext, commentID int64) (event domain.Event, err error) {
	finish := activity.Track(ctx, "app.CommentService.Remove", project, map[string]any{"comment_id": commentID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if commentID <= 0 {
		err = domain.NewError(domain.ErrValidation, "comment id must be positive", nil)
		return
	}

	existing, err := s.repo.CommentByID(ctx, project.ID, commentID)
	if err != nil {
		return
	}

	if s.workflow != nil {
		var allowed bool
		var hint string
		allowed, hint, err = s.workflow.ResolveBucketPermissions(ctx, project, existing.TaskID, EntityComment, PermissionDelete)
		if err != nil {
			return
		}
		if !allowed {
			s.workflow.EmitGuardViolated(ctx, project.ID, domain.EventEntityTask, existing.TaskID,
				GuardOperationCommentDelete, GuardRulePermissions, hint,
				map[string]any{"comment_id": commentID, "task_id": existing.TaskID, "entity": EntityComment, "operation": PermissionDelete})
			err = domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"comment_id": commentID, "task_id": existing.TaskID, "hint": hint, "entity": EntityComment, "operation": PermissionDelete})
			return
		}
	}

	event, err = s.repo.DeleteComment(ctx, project.ID, commentID)
	return
}

func (s *CommentService) List(ctx context.Context, project domain.ProjectContext, taskID int64) (comments []domain.Comment, err error) {
	finish := activity.Track(ctx, "app.CommentService.List", project, map[string]any{"task_id": taskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID < 0 {
		err = domain.NewError(domain.ErrValidation, "task id cannot be negative", nil)
		return
	}
	comments, err = s.repo.ListComments(ctx, project.ID, taskID)
	return
}

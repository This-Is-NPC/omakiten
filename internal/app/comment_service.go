package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// CommentService captures an immutable per-project Snapshot at construction.
// Tag normalization reads the bundle's synonym table through s.snap.Synonyms()
// — no setter mutates the service after construction so two projects' tables
// stay disjoint without any shared pointer between their CommentService
// instances.
type CommentService struct {
	repo     CommentRepository
	workflow *WorkflowService
	snap     *config.Snapshot
}

// NewCommentService wires the read-only flows (Add / List). snap supplies the
// per-project tag-synonym table NormalizeTagName consults; nil disables
// substitution for tests that do not exercise synonyms.
func NewCommentService(repo CommentRepository, snap *config.Snapshot) *CommentService {
	return &CommentService{repo: repo, snap: snap}
}

// NewCommentServiceWithWorkflow wires policy enforcement on Edit/Remove. The
// plain NewCommentService stays for read-only callers (Add/List) so existing
// tests don't have to thread a workflow stub everywhere. snap carries the
// per-project synonym table; production composition passes the same pointer
// the workflow service captured.
func NewCommentServiceWithWorkflow(repo CommentRepository, workflow *WorkflowService, snap *config.Snapshot) *CommentService {
	return &CommentService{repo: repo, workflow: workflow, snap: snap}
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
	if err = domain.ValidateCommentBody(body); err != nil {
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
		name := NormalizeTagName(raw, s.snap.Synonyms())
		if name == "" {
			continue
		}
		tags = append(tags, domain.Tag{Name: name, Label: TagLabel(raw)})
	}

	comment, err = s.repo.AddComment(ctx, project.ID, taskID, body, authorType, tags)
	return
}

// AddScoped creates a comment at the requested scope (task|project|universal),
// carrying the optional kind/title/pinned note-like fields. Task scope still
// requires a positive task id; project/universal scopes do not. Body and author
// validation match Add.
func (s *CommentService) AddScoped(ctx context.Context, project domain.ProjectContext, w domain.CommentWrite) (comment domain.Comment, err error) {
	finish := activity.Track(ctx, "app.CommentService.AddScoped", project, map[string]any{"scope": w.Scope, "task_id": w.TaskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	scope := w.Scope
	if scope == "" {
		scope = domain.CommentScopeTask
	}
	switch scope {
	case domain.CommentScopeTask:
		if w.TaskID <= 0 {
			err = domain.NewError(domain.ErrValidation, "task id must be positive", nil)
			return
		}
	case domain.CommentScopeProject, domain.CommentScopeUniversal:
		// no task id required
	default:
		err = domain.NewError(domain.ErrValidation, "unknown comment scope", map[string]any{"scope": w.Scope})
		return
	}

	w.Scope = scope
	w.ProjectID = project.ID
	w.Body = strings.TrimSpace(w.Body)
	if w.Body == "" {
		err = domain.NewError(domain.ErrValidation, "comment body is required", nil)
		return
	}
	if err = domain.ValidateCommentBody(w.Body); err != nil {
		return
	}
	if err = domain.ValidateCommentTitle(w.Title); err != nil {
		return
	}
	if err = domain.ValidateCommentKind(w.Kind); err != nil {
		return
	}
	w.AuthorType = strings.TrimSpace(w.AuthorType)
	if w.AuthorType == "" {
		w.AuthorType = "human"
	}
	if w.AuthorType != "human" && w.AuthorType != "agent" {
		err = domain.NewError(domain.ErrValidation, "author type must be human or agent", map[string]any{"author_type": w.AuthorType})
		return
	}
	w.Tags = s.normalizeTags(w.Tags)

	if err = s.enforceCommentCreatePermission(ctx, project, scope, w.TaskID, tagNames(w.Tags)); err != nil {
		return
	}

	comment, err = s.repo.AddScopedComment(ctx, w)
	return
}

// enforceCommentCreatePermission is the scope-aware create guard, mirroring
// enforceCommentPermission (edit/remove) but keyed on the write intent's scope
// and task id rather than an existing comment row. It is a no-op when no
// workflow service is wired (read-only composition) so legacy callers stay
// permissive (no rule = allow, back-compat).
//
//   - task scope:      bucket-resolved create permission via
//     ResolveBucketPermissions(taskID, EntityComment, PermissionCreate). A
//     denial emits a task-scoped guard.violated and returns ErrGuardViolation.
//   - project scope:   resolved task-lessly (defaults.comment.project.create);
//     a denial emits a project-scoped guard.violated.
//   - universal scope: resolved task-lessly (defaults.comment.universal.create);
//     a denial emits a project-less (universal) guard.violated.
func (s *CommentService) enforceCommentCreatePermission(ctx context.Context, project domain.ProjectContext, scope string, taskID int64, tags []string) error {
	if s.workflow == nil {
		return nil
	}
	if scope == "" {
		scope = domain.CommentScopeTask
	}

	if scope == domain.CommentScopeTask {
		allowed, hint, err := s.workflow.ResolveCommentBucketPolicy(ctx, project, taskID, PermissionCreate, tags)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
		taskRow, taskSnap, taskErr := s.workflow.ResolveTaskSnap(ctx, project, taskID)
		if taskErr != nil {
			return taskErr
		}
		s.workflow.Evaluator().EmitViolatedForTask(ctx, project.ID, taskRow, taskSnap,
			GuardOperationCommentCreate, GuardRulePermissions, hint,
			map[string]any{"task_id": taskID, "entity": EntityComment, "operation": PermissionCreate})
		return domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"task_id": taskID, "hint": hint, "entity": EntityComment, "operation": PermissionCreate})
	}

	// project / universal: no task, resolve against workflow defaults only.
	allowed, hint := s.workflow.ResolveCommentScopePolicy(scope, PermissionCreate, tags)
	if allowed {
		return nil
	}
	target := map[string]any{"entity": EntityComment, "operation": PermissionCreate, "scope": scope}
	if scope == domain.CommentScopeProject {
		s.workflow.Evaluator().EmitViolatedForProject(ctx, project.ID, GuardOperationCommentCreate, GuardRulePermissions, hint, target)
	} else {
		// Universal comments are stored project-less (project_id IS NULL); the
		// violation row must match, so pass projectID=0.
		s.workflow.Evaluator().EmitViolated(ctx, 0, domain.EventEntityUniversal, 0, GuardOperationCommentCreate, GuardRulePermissions, hint, target)
	}
	return domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"hint": hint, "entity": EntityComment, "operation": PermissionCreate, "scope": scope})
}

// Query runs the filterable handoff-log read (scope/kind/tag/FTS/pinned/window,
// single-project or cross-project). The filter is passed through to the repo
// untouched; callers set ProjectID explicitly to scope or leave it 0 for the
// cross-project view.
func (s *CommentService) Query(ctx context.Context, project domain.ProjectContext, filter domain.CommentFilter) (comments []domain.Comment, err error) {
	if filter.Search != "" {
		filter.Search, err = domain.ValidateSearchQuery(filter.Search)
		if err != nil {
			return nil, err
		}
	}
	finish := activity.Track(ctx, "app.CommentService.Query", project, map[string]any{"scope": filter.Scope, "kind": filter.Kind})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()
	comments, err = s.repo.QueryComments(ctx, filter)
	return
}

// tagNames projects a tag slice to its normalized names — the form the policy
// predicates (require_tags / deny_tags) compare against. Used to thread the
// create payload's tags and an existing comment's stored tags into comment
// permission resolution.
func tagNames(tags []domain.Tag) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

// normalizeTags applies the per-project synonym table to a tag slice. Tags that
// normalize to empty are dropped. Mirrors the inline loops in Add/Edit so the
// scope-aware writers share one path.
func (s *CommentService) normalizeTags(in []domain.Tag) []domain.Tag {
	out := make([]domain.Tag, 0, len(in))
	for _, t := range in {
		raw := t.Label
		if raw == "" {
			raw = t.Name
		}
		name := NormalizeTagName(raw, s.snap.Synonyms())
		if name == "" {
			continue
		}
		out = append(out, domain.Tag{Name: name, Label: TagLabel(raw)})
	}
	return out
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
	if err = domain.ValidateCommentBody(body); err != nil {
		return
	}

	existing, err := s.repo.CommentByID(ctx, project.ID, commentID)
	if err != nil {
		return
	}

	if err = s.enforceCommentPermission(ctx, project, existing, commentID, PermissionEdit, GuardOperationCommentEdit); err != nil {
		return
	}

	tags := make([]domain.Tag, 0, len(rawTags))
	for _, raw := range rawTags {
		name := NormalizeTagName(raw, s.snap.Synonyms())
		if name == "" {
			continue
		}
		tags = append(tags, domain.Tag{Name: name, Label: TagLabel(raw)})
	}

	comment, _, err = s.repo.UpdateComment(ctx, project.ID, commentID, body, tags)
	return
}

// EditScoped applies the full scope-agnostic patch (body/title/kind/pinned +
// tags) through repo.EditComment after enforcing the per-bucket comment.edit
// policy. It mirrors Edit's validation and permission path but carries the
// note-like columns introduced for the scoped comment surface.
// rawTags is tri-state: a nil pointer means "tags omitted" and leaves the
// comment's existing tag set untouched, while a non-nil pointer (even an empty
// slice) replaces the tags wholesale. This keeps a body-only or metadata-only
// edit from silently wiping a comment's tags.
func (s *CommentService) EditScoped(ctx context.Context, project domain.ProjectContext, commentID int64, edit domain.CommentEdit, rawTags *[]string) (comment domain.Comment, err error) {
	finish := activity.Track(ctx, "app.CommentService.EditScoped", project, map[string]any{"comment_id": commentID})
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
	// Body is tri-state: a nil pointer leaves the stored body untouched (a
	// metadata-only edit), while a non-nil pointer overwrites it but must be
	// non-empty after trim — you can rewrite a body but not blank it.
	if edit.Body != nil {
		trimmed := strings.TrimSpace(*edit.Body)
		if trimmed == "" {
			err = domain.NewError(domain.ErrValidation, "comment body is required", nil)
			return
		}
		if err = domain.ValidateCommentBody(trimmed); err != nil {
			return
		}
		edit.Body = &trimmed
	}
	if edit.Title != nil {
		if err = domain.ValidateCommentTitle(*edit.Title); err != nil {
			return
		}
	}
	if edit.Kind != nil {
		if err = domain.ValidateCommentKind(*edit.Kind); err != nil {
			return
		}
	}

	// Reject a no-op patch: an edit that changes nothing (no body, no
	// title/kind/pinned, no tags) is not a real edit. Tri-state body must not
	// silently let an empty patch through. A non-nil rawTags pointer counts as a
	// provided field even when the slice is empty (an explicit tag clear).
	if edit.Body == nil && edit.Title == nil && edit.Kind == nil && edit.Pinned == nil && rawTags == nil {
		err = domain.NewError(domain.ErrValidation, "comment edit requires at least one field", nil)
		return
	}

	existing, err := s.repo.CommentByID(ctx, project.ID, commentID)
	if err != nil {
		return
	}

	if err = s.enforceCommentPermission(ctx, project, existing, commentID, PermissionEdit, GuardOperationCommentEdit); err != nil {
		return
	}

	// Only normalize and forward tags when the caller provided them; a nil
	// rawTags pointer leaves edit.Tags nil so the store preserves the existing
	// tag set (tri-state).
	if rawTags != nil {
		tags := make([]domain.Tag, 0, len(*rawTags))
		for _, raw := range *rawTags {
			name := NormalizeTagName(raw, s.snap.Synonyms())
			if name == "" {
				continue
			}
			tags = append(tags, domain.Tag{Name: name, Label: TagLabel(raw)})
		}
		edit.Tags = &tags
	}

	comment, _, err = s.repo.EditComment(ctx, project.ID, commentID, edit)
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

	if err = s.enforceCommentPermission(ctx, project, existing, commentID, PermissionDelete, GuardOperationCommentDelete); err != nil {
		return
	}

	event, err = s.repo.DeleteComment(ctx, project.ID, commentID)
	return
}

// enforceCommentPermission is the scope-aware guard shared by Edit and Remove.
// It is a no-op when no workflow service is wired (read-only composition).
//
//   - task scope:      resolved against the comment's current bucket via
//     ResolveBucketPermissions (uses the comment's TaskID). A denial emits a
//     task-scoped guard.violated and returns ErrGuardViolation.
//   - project scope:   resolved task-lessly against the workflow defaults
//     (defaults.comment.project.<op>). A denial emits a project-scoped
//     guard.violated and returns ErrGuardViolation.
//   - universal scope: resolved task-lessly (defaults.comment.universal.<op>).
//     A denial emits a global (entity-less) guard.violated and returns
//     ErrGuardViolation.
//
// operation is PermissionEdit/PermissionDelete; guardOp is the canonical
// GuardOperationComment* payload value.
func (s *CommentService) enforceCommentPermission(ctx context.Context, project domain.ProjectContext, existing domain.Comment, commentID int64, operation, guardOp string) error {
	if s.workflow == nil {
		return nil
	}

	scope := existing.Scope
	if scope == "" {
		scope = domain.CommentScopeTask
	}

	storedTags := tagNames(existing.Tags)

	if scope == domain.CommentScopeTask {
		allowed, hint, err := s.workflow.ResolveCommentBucketPolicy(ctx, project, existing.TaskID, operation, storedTags)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
		taskRow, taskSnap, taskErr := s.workflow.ResolveTaskSnap(ctx, project, existing.TaskID)
		if taskErr != nil {
			return taskErr
		}
		s.workflow.Evaluator().EmitViolatedForTask(ctx, project.ID, taskRow, taskSnap,
			guardOp, GuardRulePermissions, hint,
			map[string]any{"comment_id": commentID, "task_id": existing.TaskID, "entity": EntityComment, "operation": operation})
		return domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"comment_id": commentID, "task_id": existing.TaskID, "hint": hint, "entity": EntityComment, "operation": operation})
	}

	// project / universal: no task, resolve against workflow defaults only.
	allowed, hint := s.workflow.ResolveCommentScopePolicy(scope, operation, storedTags)
	if allowed {
		return nil
	}
	target := map[string]any{"comment_id": commentID, "entity": EntityComment, "operation": operation, "scope": scope}
	if scope == domain.CommentScopeProject {
		s.workflow.Evaluator().EmitViolatedForProject(ctx, project.ID, guardOp, GuardRulePermissions, hint, target)
	} else {
		// Universal comments are stored project-less (project_id IS NULL); the
		// violation row must match, so pass projectID=0 rather than stamping the
		// acting project onto a project-less entity.
		s.workflow.Evaluator().EmitViolated(ctx, 0, domain.EventEntityUniversal, 0, guardOp, GuardRulePermissions, hint, target)
	}
	return domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"comment_id": commentID, "hint": hint, "entity": EntityComment, "operation": operation, "scope": scope})
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

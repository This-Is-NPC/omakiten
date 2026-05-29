package domain

type Workflow struct {
	ID          int64                `json:"id"`
	Key         string               `json:"key"`
	Name        string               `json:"name"`
	Buckets     []Bucket             `json:"buckets,omitempty"`
	Transitions []WorkflowTransition `json:"transitions,omitempty"`
	Operations  WorkflowOperations   `json:"operations,omitempty"`
	// Defaults declares the workflow-level fallback for task/comment edit
	// and delete. A nil pointer means "no rule declared at the workflow
	// level" — bucket overrides are still consulted, and when neither
	// bucket nor workflow declares a value the final fallback is `true`
	// (no rule = allow). Authors who want a strict default declare it
	// explicitly here.
	Defaults *WorkflowDefaults `json:"defaults,omitempty"`
}

// WorkflowDefaults is the policy fallback applied when a bucket does not
// override the field. Task is the base; Comment, when nil, inherits from
// Task field-by-field, mirroring the per-bucket inheritance rule.
type WorkflowDefaults struct {
	Task    *EntityPermission `json:"task,omitempty"`
	Comment *EntityPermission `json:"comment,omitempty"`
}

type Bucket struct {
	ID          int64              `json:"id"`
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Position    int                `json:"position"`
	Permissions *BucketPermissions `json:"permissions,omitempty"`
}

// BucketPermissions wires task/comment CRUD policy at the bucket level.
// nil pointers mean "no override at this layer" — the resolver falls back
// to workflow.Defaults, then to the implicit `true` (no rule = allow).
// Comment inherits from Task field-by-field at every layer of the chain.
type BucketPermissions struct {
	Task    *EntityPermission `json:"task,omitempty"`
	Comment *EntityPermission `json:"comment,omitempty"`
}

type EntityPermission struct {
	Edit   *bool `json:"edit,omitempty"`
	Delete *bool `json:"delete,omitempty"`
	// Scopes carries optional per-scope sub-blocks for the comment entity
	// only (task|project|universal). Task scope reuses the flat Edit/Delete
	// fields for backward-compat: a flat `comment: {edit,delete}` resolves
	// as the task scope. Project/Universal comments have no bucket, so they
	// resolve purely at the workflow-defaults level via these sub-blocks.
	// nil sub-blocks fall through to the implicit `true`.
	Task      *EntityPermission `json:"task,omitempty"`
	Project   *EntityPermission `json:"project,omitempty"`
	Universal *EntityPermission `json:"universal,omitempty"`
}

// CommentScopeEdit / CommentScopeDelete are the op selectors ResolveCommentScopePermission
// arbitrates over. Mirror the PermissionEdit/Delete strings in the app layer.
const (
	commentOpEdit   = "edit"
	commentOpDelete = "delete"
)

// WorkflowOperations declares guards that gate non-flow operations
// (archive / delete / unarchive). Reuses TransitionGuard so the existing
// guard evaluator covers operation guards without a new guard type.
type WorkflowOperations struct {
	Archive   OperationPolicy `json:"archive,omitempty"`
	Delete    OperationPolicy `json:"delete,omitempty"`
	Unarchive OperationPolicy `json:"unarchive,omitempty"`
}

type OperationPolicy struct {
	Guards []TransitionGuard `json:"guards,omitempty"`
}

// ResolveTaskPermission returns the effective task-level (edit, delete)
// for this bucket given the workflow-level defaults. Resolution chain:
//  1. bucket.permissions.task.<field>      — most specific.
//  2. workflow.defaults.task.<field>       — workflow-level fallback.
//  3. true                                 — implicit fallback (no rule = allow).
//
// Defaults may be nil; nil means "no workflow-level rule declared" and the
// resolver falls straight to step 3. There is no hardcoded "first bucket
// is special" rule — the entire policy is data-driven.
func (b Bucket) ResolveTaskPermission(defaults *WorkflowDefaults) (edit, del bool) {
	edit = resolveBool(
		bucketField(b, taskEdit),
		defaultsField(defaults, taskEdit),
	)
	del = resolveBool(
		bucketField(b, taskDelete),
		defaultsField(defaults, taskDelete),
	)
	return edit, del
}

// ResolveCommentPermission returns the effective task-scope comment-level
// (edit, delete) for this bucket. Comment fields inherit from task at each
// layer of the chain — bucket.comment falls back to bucket.task; if both
// are nil, defaults.comment falls back to defaults.task; if all four are
// nil the implicit `true` wins. This is the task-scope path; project and
// universal comments have no bucket and resolve via
// ResolveCommentScopePermission against the workflow defaults directly.
func (b Bucket) ResolveCommentPermission(defaults *WorkflowDefaults) (edit, del bool) {
	edit = resolveBool(
		bucketField(b, commentEdit),
		bucketField(b, taskEdit),
		defaultsField(defaults, commentEdit),
		defaultsField(defaults, taskEdit),
	)
	del = resolveBool(
		bucketField(b, commentDelete),
		bucketField(b, taskDelete),
		defaultsField(defaults, commentDelete),
		defaultsField(defaults, taskDelete),
	)
	return edit, del
}

// ResolveCommentScopePermission resolves the comment edit/delete policy for a
// given scope (task|project|universal) against the workflow defaults, with no
// bucket layer. Scope resolution chains:
//
//	task:      defaults.comment.task.<op> → defaults.comment.<op> → defaults.task.<op> → true
//	project:   defaults.comment.project.<op> → true
//	universal: defaults.comment.universal.<op> → true
//
// The task scope keeps the flat `comment: {edit,delete}` fields as a
// backward-compatible alias for `comment.task` and still inherits from
// defaults.task, mirroring the per-bucket task chain at the defaults layer.
// Project/Universal have no bucket and no task inheritance — an undeclared
// sub-block falls straight through to the implicit `true` (no rule = allow).
func ResolveCommentScopePermission(defaults *WorkflowDefaults, scope, op string) bool {
	var comment *EntityPermission
	if defaults != nil {
		comment = defaults.Comment
	}
	switch scope {
	case CommentScopeProject:
		return resolveBool(scopeOpField(scopeBlock(comment, CommentScopeProject), op))
	case CommentScopeUniversal:
		return resolveBool(scopeOpField(scopeBlock(comment, CommentScopeUniversal), op))
	default: // task scope (also the implicit/empty fallback)
		return resolveBool(
			scopeOpField(scopeBlock(comment, CommentScopeTask), op),
			flatOpField(comment, op),
			defaultsTaskOpField(defaults, op),
		)
	}
}

// scopeBlock returns the named scope sub-block on a comment EntityPermission,
// or nil when the comment block or sub-block is absent.
func scopeBlock(comment *EntityPermission, scope string) *EntityPermission {
	if comment == nil {
		return nil
	}
	switch scope {
	case CommentScopeTask:
		return comment.Task
	case CommentScopeProject:
		return comment.Project
	case CommentScopeUniversal:
		return comment.Universal
	}
	return nil
}

// scopeOpField extracts the edit/delete pointer from a scope sub-block.
func scopeOpField(p *EntityPermission, op string) *bool {
	if p == nil {
		return nil
	}
	if op == commentOpDelete {
		return p.Delete
	}
	return p.Edit
}

// flatOpField extracts the flat edit/delete pointer from the comment block —
// the backward-compatible `comment: {edit,delete}` alias for the task scope.
func flatOpField(comment *EntityPermission, op string) *bool {
	if comment == nil {
		return nil
	}
	if op == commentOpDelete {
		return comment.Delete
	}
	return comment.Edit
}

// defaultsTaskOpField extracts the defaults.task edit/delete pointer so the
// task-scope comment chain inherits from defaults.task as the final declared
// layer before the implicit true.
func defaultsTaskOpField(defaults *WorkflowDefaults, op string) *bool {
	if defaults == nil || defaults.Task == nil {
		return nil
	}
	if op == commentOpDelete {
		return defaults.Task.Delete
	}
	return defaults.Task.Edit
}

// resolveBool walks the candidate pointers in priority order and returns
// the first non-nil value. When every candidate is nil the implicit
// fallback is `true` — the documented "no rule = allow" semantics.
func resolveBool(candidates ...*bool) bool {
	for _, c := range candidates {
		if c != nil {
			return *c
		}
	}
	return true
}

// permField is a small enum used by bucketField/defaultsField to pick
// which (entity, op) pointer to extract without duplicating four nearly
// identical lookup functions.
type permField int

const (
	taskEdit permField = iota
	taskDelete
	commentEdit
	commentDelete
)

func bucketField(b Bucket, f permField) *bool {
	if b.Permissions == nil {
		return nil
	}
	return entityField(b.Permissions.Task, b.Permissions.Comment, f)
}

func defaultsField(d *WorkflowDefaults, f permField) *bool {
	if d == nil {
		return nil
	}
	return entityField(d.Task, d.Comment, f)
}

func entityField(task, comment *EntityPermission, f permField) *bool {
	switch f {
	case taskEdit:
		if task == nil {
			return nil
		}
		return task.Edit
	case taskDelete:
		if task == nil {
			return nil
		}
		return task.Delete
	case commentEdit:
		if comment == nil {
			return nil
		}
		return comment.Edit
	case commentDelete:
		if comment == nil {
			return nil
		}
		return comment.Delete
	}
	return nil
}

// FinalBucketKey returns the key of the highest-position bucket — the
// "completed / final" lane in the resolved workflow shape. Used by callers
// that need to classify "open work" vs "shipped" without hardcoding a
// bucket name. Returns "" when the workflow has no buckets so callers
// can fall back to a degraded count.
func (w Workflow) FinalBucketKey() string {
	if len(w.Buckets) == 0 {
		return ""
	}
	final := w.Buckets[0]
	for _, b := range w.Buckets {
		if b.Position > final.Position {
			final = b
		}
	}
	return final.Key
}

type WorkflowTransition struct {
	FromBucketID  int64  `json:"from_bucket_id"`
	FromBucketKey string `json:"from_bucket_key"`
	ToBucketID    int64  `json:"to_bucket_id"`
	ToBucketKey   string `json:"to_bucket_key"`
}

// TransitionGuard is a rule attached to a workflow transition. Type discriminates
// the payload: "blockers_in" reads Buckets, "comments_min" reads Count,
// "comments_tagged" reads Tag (and optionally Count). Hint is surfaced verbatim
// in the guard violation error so authors can give the user a remediation tip.
type TransitionGuard struct {
	Type    string   `json:"type"`
	Buckets []string `json:"buckets,omitempty"`
	Count   int      `json:"count,omitempty"`
	Tag     string   `json:"tag,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

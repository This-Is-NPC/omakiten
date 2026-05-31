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
	// Create/Edit/Delete are CommentOpPolicy values (uniform representation).
	// For the comment entity they carry the full polymorphic rule (allow +
	// tag predicates). For the task entity only the base Allow verdict is
	// ever read; the config validator rejects tag predicates declared under
	// permissions.task.*, so task-permission semantics stay plain bool.
	Create *CommentOpPolicy `json:"create,omitempty"`
	Edit   *CommentOpPolicy `json:"edit,omitempty"`
	Delete *CommentOpPolicy `json:"delete,omitempty"`
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

// CommentOpCreate / CommentOpEdit / CommentOpDelete are the only supported
// comment policy operation selectors. App-layer Permission* constants mirror
// these values; keeping the symbols in the domain package prevents unknown
// operation strings from silently falling into edit semantics.
const (
	CommentOpCreate = "create"
	CommentOpEdit   = "edit"
	CommentOpDelete = "delete"
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
	edit = resolvePolicyBool(
		bucketField(b, taskEdit),
		defaultsField(defaults, taskEdit),
	)
	del = resolvePolicyBool(
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
	return b.ResolveCommentPolicy(defaults, CommentOpEdit).Evaluate(nil),
		b.ResolveCommentPolicy(defaults, CommentOpDelete).Evaluate(nil)
}

// ResolveCommentPolicy returns the effective task-scope comment policy for the
// given op (create|edit|delete) as a CommentOpPolicy, returning the winning
// rule object instead of a pre-evaluated bool. Callers thread the operation's
// relevant tags into Evaluate to apply the require/deny predicates. Edit/delete
// comment fields inherit from task at each layer; create delegates to the
// comment-only create chain.
func (b Bucket) ResolveCommentPolicy(defaults *WorkflowDefaults, op string) CommentOpPolicy {
	if op == CommentOpCreate {
		return b.ResolveCommentCreatePolicy(defaults)
	}
	comment, ok := commentField(op)
	if !ok {
		return boolPolicy(false)
	}
	task, ok := taskField(op)
	if !ok {
		return boolPolicy(false)
	}
	return resolveCommentPolicy(
		bucketField(b, comment),
		bucketField(b, task),
		defaultsCommentScopeField(defaults, CommentScopeTask, op),
		defaultsField(defaults, comment),
		defaultsField(defaults, task),
	)
}

// ResolveCommentCreatePermission returns the effective task-scope comment
// create permission for this bucket. Create is comment-only — tasks have no
// create permission — so the chain skips the task-entity fallbacks that
// ResolveCommentPermission walks for edit/delete:
//
//	bucket.comment.create → defaults.comment.task.create → defaults.comment.create → true
//
// Project- and universal-scoped comments have no bucket and resolve via
// ResolveCommentScopePermission(scope, "create") instead.
func (b Bucket) ResolveCommentCreatePermission(defaults *WorkflowDefaults) bool {
	return b.ResolveCommentCreatePolicy(defaults).Evaluate(nil)
}

// ResolveCommentCreatePolicy returns the effective task-scope comment create
// policy as a CommentOpPolicy so callers can thread the request payload tags
// into Evaluate. Create is comment-only — the chain skips the task-entity
// fallbacks ResolveCommentPolicy walks for edit/delete.
func (b Bucket) ResolveCommentCreatePolicy(defaults *WorkflowDefaults) CommentOpPolicy {
	return resolveCommentPolicy(
		bucketField(b, commentCreate),
		defaultsCommentScopeField(defaults, CommentScopeTask, CommentOpCreate),
		defaultsField(defaults, commentCreate),
	)
}

// defaultsCommentScopeField extracts defaults.comment.<scope>.<op>, the
// per-scope sub-block on the workflow defaults. Used by the task-comment bucket
// chain so a defaults-driven `defaults.comment.task.{edit,delete}` is honored
// even when no bucket declares a comment/task override (the #389 designed
// chain). Returns nil when any layer of the path is absent.
func defaultsCommentScopeField(defaults *WorkflowDefaults, scope, op string) *CommentOpPolicy {
	if defaults == nil {
		return nil
	}
	return scopeOpField(scopeBlock(defaults.Comment, scope), op)
}

// ResolveCommentScopePermission resolves the comment create/edit/delete policy for a
// given scope (task|project|universal) against the workflow defaults, with no
// bucket layer. Scope resolution chains:
//
//	task:      defaults.comment.task.<op> → defaults.comment.<op> → defaults.task.<op> → true
//	project:   defaults.comment.project.<op> → true
//	universal: defaults.comment.universal.<op> → true
//
// Create skips the defaults.task fallback because tasks have no create permission.
// The task scope keeps the flat `comment: {edit,delete}` fields as a
// backward-compatible alias for `comment.task` and still inherits from
// defaults.task, mirroring the per-bucket task chain at the defaults layer.
// Project/Universal have no bucket and no task inheritance — an undeclared
// sub-block falls straight through to the implicit `true` (no rule = allow).
func ResolveCommentScopePermission(defaults *WorkflowDefaults, scope, op string) bool {
	return ResolveCommentScopePolicy(defaults, scope, op).Evaluate(nil)
}

// ResolveCommentScopePolicy resolves the comment policy for a given scope/op
// against the workflow defaults (no bucket layer) and returns the winning
// CommentOpPolicy so callers can thread the relevant tag set into Evaluate.
// Scope chains match ResolveCommentScopePermission; most-specific declared
// layer wins, with the implicit allowing policy as terminal.
func ResolveCommentScopePolicy(defaults *WorkflowDefaults, scope, op string) CommentOpPolicy {
	if !validCommentOp(op) {
		return boolPolicy(false)
	}
	var comment *EntityPermission
	if defaults != nil {
		comment = defaults.Comment
	}
	switch scope {
	case CommentScopeProject:
		return resolveCommentPolicy(scopeOpField(scopeBlock(comment, CommentScopeProject), op))
	case CommentScopeUniversal:
		return resolveCommentPolicy(scopeOpField(scopeBlock(comment, CommentScopeUniversal), op))
	default: // task scope (also the implicit/empty fallback)
		return resolveCommentPolicy(
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

// scopeOpField extracts the create/edit/delete policy pointer from a scope
// sub-block.
func scopeOpField(p *EntityPermission, op string) *CommentOpPolicy {
	if p == nil {
		return nil
	}
	switch op {
	case CommentOpCreate:
		return p.Create
	case CommentOpEdit:
		return p.Edit
	case CommentOpDelete:
		return p.Delete
	}
	return nil
}

// flatOpField extracts the flat create/edit/delete policy pointer from the comment
// block — the backward-compatible `comment: {edit,delete}` alias for the task
// scope.
func flatOpField(comment *EntityPermission, op string) *CommentOpPolicy {
	if comment == nil {
		return nil
	}
	switch op {
	case CommentOpCreate:
		return comment.Create
	case CommentOpEdit:
		return comment.Edit
	case CommentOpDelete:
		return comment.Delete
	}
	return nil
}

// defaultsTaskOpField extracts the defaults.task edit/delete policy pointer so
// the task-scope comment chain inherits from defaults.task as the final
// declared layer before the implicit true.
func defaultsTaskOpField(defaults *WorkflowDefaults, op string) *CommentOpPolicy {
	if defaults == nil || defaults.Task == nil {
		return nil
	}
	switch op {
	case CommentOpCreate:
		// Tasks have no create permission, so the comment-create chain never
		// inherits from defaults.task — only the comment layers apply.
		return nil
	case CommentOpEdit:
		return defaults.Task.Edit
	case CommentOpDelete:
		return defaults.Task.Delete
	}
	return nil
}

// commentField / taskField map an op string (create|edit|delete) to the
// permField selector for the comment / task entity, so the policy resolvers can
// drive the shared bucketField/defaultsField extractors without a per-op
// switch at every call site.
func commentField(op string) (permField, bool) {
	switch op {
	case CommentOpCreate:
		return commentCreate, true
	case CommentOpEdit:
		return commentEdit, true
	case CommentOpDelete:
		return commentDelete, true
	}
	return 0, false
}

func taskField(op string) (permField, bool) {
	switch op {
	case CommentOpEdit:
		return taskEdit, true
	case CommentOpDelete:
		return taskDelete, true
	}
	return 0, false
}

func validCommentOp(op string) bool {
	switch op {
	case CommentOpCreate, CommentOpEdit, CommentOpDelete:
		return true
	default:
		return false
	}
}

// resolvePolicyBool resolves a chain of policy pointers to a plain bool — the
// task-permission path, which reads only the base Allow verdict (no tag
// predicates). Mirrors the legacy resolveBool: first declared layer wins,
// implicit fallback true.
func resolvePolicyBool(candidates ...*CommentOpPolicy) bool {
	return resolveCommentPolicy(candidates...).Evaluate(nil)
}

// permField is a small enum used by bucketField/defaultsField to pick
// which (entity, op) pointer to extract without duplicating four nearly
// identical lookup functions.
type permField int

const (
	taskEdit permField = iota
	taskDelete
	commentCreate
	commentEdit
	commentDelete
)

func bucketField(b Bucket, f permField) *CommentOpPolicy {
	if b.Permissions == nil {
		return nil
	}
	return entityField(b.Permissions.Task, b.Permissions.Comment, f)
}

func defaultsField(d *WorkflowDefaults, f permField) *CommentOpPolicy {
	if d == nil {
		return nil
	}
	return entityField(d.Task, d.Comment, f)
}

func entityField(task, comment *EntityPermission, f permField) *CommentOpPolicy {
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
	case commentCreate:
		if comment == nil {
			return nil
		}
		return comment.Create
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

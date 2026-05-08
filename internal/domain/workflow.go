package domain

type Workflow struct {
	ID          int64                `json:"id"`
	Key         string               `json:"key"`
	Name        string               `json:"name"`
	Buckets     []Bucket             `json:"buckets,omitempty"`
	Transitions []WorkflowTransition `json:"transitions,omitempty"`
	Operations  WorkflowOperations   `json:"operations,omitempty"`
}

type Bucket struct {
	ID          int64              `json:"id"`
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Position    int                `json:"position"`
	Permissions *BucketPermissions `json:"permissions,omitempty"`
}

// BucketPermissions wires task/comment CRUD policy. nil pointers mean the
// canonical default applies (edit=true on first bucket, false elsewhere;
// delete=false everywhere). Comment falls back to task when its sub-block is
// absent or partially set.
type BucketPermissions struct {
	Task    *EntityPermission `json:"task,omitempty"`
	Comment *EntityPermission `json:"comment,omitempty"`
}

type EntityPermission struct {
	Edit   *bool `json:"edit,omitempty"`
	Delete *bool `json:"delete,omitempty"`
}

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

// ResolveTaskPermission returns the effective task-level (edit, delete) for
// the bucket honoring the canonical defaults: edit=true on the first bucket
// (position 1) and false elsewhere; delete=false everywhere. The bucketIsFirst
// flag tells the resolver whether the bucket sits at position 1 — the runtime
// supplies it because the bucket alone does not know its workflow context.
func (b Bucket) ResolveTaskPermission(bucketIsFirst bool) (edit, del bool) {
	edit = bucketIsFirst
	del = false
	if b.Permissions != nil && b.Permissions.Task != nil {
		if b.Permissions.Task.Edit != nil {
			edit = *b.Permissions.Task.Edit
		}
		if b.Permissions.Task.Delete != nil {
			del = *b.Permissions.Task.Delete
		}
	}
	return edit, del
}

// ResolveCommentPermission returns the effective comment-level (edit, delete)
// for the bucket. When `permissions.comment` is missing, both fields inherit
// from `permissions.task`. When present partially, only the explicitly set
// fields override.
func (b Bucket) ResolveCommentPermission(bucketIsFirst bool) (edit, del bool) {
	edit, del = b.ResolveTaskPermission(bucketIsFirst)
	if b.Permissions != nil && b.Permissions.Comment != nil {
		if b.Permissions.Comment.Edit != nil {
			edit = *b.Permissions.Comment.Edit
		}
		if b.Permissions.Comment.Delete != nil {
			del = *b.Permissions.Comment.Delete
		}
	}
	return edit, del
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

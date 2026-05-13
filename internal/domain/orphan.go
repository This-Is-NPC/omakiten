package domain

// OrphanedTask describes a single task whose current bucket no longer exists
// in the active workflow. The task can be rebinded to a target bucket — either
// the same key in the new workflow (when preserved) or the first active bucket
// (when the key was removed).
type OrphanedTask struct {
	TaskID        int64  `json:"task_id"`
	Title         string `json:"title"`
	FromBucketKey string `json:"from_bucket_key"`
	ToBucketKey   string `json:"to_bucket_key"`
}

// OrphanGroup aggregates orphaned tasks that share the same from→to rebind.
// Keeps the preview compact when many tasks share the same fate.
type OrphanGroup struct {
	FromBucketKey string         `json:"from_bucket_key"`
	ToBucketKey   string         `json:"to_bucket_key"`
	Count         int            `json:"count"`
	Tasks         []OrphanedTask `json:"tasks,omitempty"`
}

// OrphanReport is the preview/result of a workflow-orphan migration. Empty
// Groups means no rebind needed — either no orphans or the workflow hasn't
// changed in a way that affects existing tasks.
type OrphanReport struct {
	WorkflowKey string        `json:"workflow_key"`
	Groups      []OrphanGroup `json:"groups,omitempty"`
	Total       int           `json:"total"`
}

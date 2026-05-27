package domain

import "encoding/json"

// OrphanedTask describes a single task whose current bucket no longer exists
// in the active workflow. The task can be rebinded to a target bucket — either
// the same key in the new workflow (when preserved) or the first active bucket
// (when the key was removed).
type OrphanedTask struct {
	TaskID        int64  `json:"task_id"`
	Title         string `json:"title"`
	FromBucketKey string `json:"from_bucket_key"`
	ToBucketKey   string `json:"to_bucket_key"`
	// ParentID + Depth surface the task's position in the parent_id tree
	// so the sub-task kit cascade migration (#285) can attribute orphan
	// events to the correct resolved kit. ParentID is nil for root tasks;
	// Depth is 0 for root rows, 1 for direct sub-tasks, 2 for grandchildren.
	ParentID *int64 `json:"parent_id,omitempty"`
	Depth    int    `json:"depth,omitempty"`
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

// TaskBucketOrphanedPayload is the locked event payload emitted by the
// sub-task kit cascade migration per affected sub-task: task id, parent
// id (nullable), depth, the bucket key the task held before migration,
// the kit identities that bracket the swap, and the structured reason.
// Moved to the domain package per #297 review opportunity §D.19 / #299
// §C so the JSON contract sits next to every other event-payload
// schema (audit consumers, hook payload templates, MCP adapters all
// read against this shape). JSON tags are byte-identical to the
// pre-move struct in sqlite/orphans.go so existing rows stay
// compatible.
type TaskBucketOrphanedPayload struct {
	TaskID      int64  `json:"task_id"`
	ParentID    *int64 `json:"parent_id"`
	Depth       int    `json:"depth"`
	OldBucket   string `json:"old_bucket"`
	FromKit     string `json:"from_kit"`
	ToKit       string `json:"to_kit"`
	ResolvedKit string `json:"resolved_kit"`
	Reason      string `json:"reason"`
}

func (p TaskBucketOrphanedPayload) JSON() (string, error) {
	return payloadJSON(p)
}

// TaskMigratedPayload is the legacy task.migrated payload — kept on
// its own typed struct so future schema changes round-trip safely
// through encoding/json. Moved to domain alongside
// TaskBucketOrphanedPayload (#297 §D.19 / #299 §C).
type TaskMigratedPayload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

func (p TaskMigratedPayload) JSON() (string, error) {
	return payloadJSON(p)
}

// SubtaskKitNoticePayload is the audit-log shape for the one-shot
// transparency notice fired by the bundle-cache rebuild on first
// enablement of subtask_kit. Carries the i18n key plus the kit
// identities bracketing the transition. Moved to domain so the
// agentruntime cache layer no longer owns the JSON contract (#297
// §D.19 / #299 §C).
type SubtaskKitNoticePayload struct {
	I18nKey string `json:"i18n_key"`
	FromKit string `json:"from_kit"`
	ToKit   string `json:"to_kit"`
}

func (p SubtaskKitNoticePayload) JSON() (string, error) {
	return payloadJSON(p)
}

func payloadJSON(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

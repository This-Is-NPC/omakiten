package domain

type Workflow struct {
	ID          int64                `json:"id"`
	Key         string               `json:"key"`
	Name        string               `json:"name"`
	Buckets     []Bucket             `json:"buckets,omitempty"`
	Transitions []WorkflowTransition `json:"transitions,omitempty"`
}

type Bucket struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
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

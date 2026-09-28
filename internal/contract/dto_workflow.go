package contract

type WorkflowSummary struct {
	Key         string              `json:"key"`
	Name        string              `json:"name"`
	Buckets     []BucketSummary     `json:"buckets,omitempty"`
	Transitions []TransitionSummary `json:"transitions,omitempty"`
}

type BucketSummary struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type TransitionSummary struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type WorkflowInput struct {
	ProjectSelector
}

type WorkflowResponse struct {
	Project  ProjectSummary  `json:"project"`
	Workflow WorkflowSummary `json:"workflow"`
}

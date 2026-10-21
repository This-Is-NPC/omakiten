package contract

import "omakiten/internal/domain"

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

// TransitionSummary is one declared transition. Ready and Guards are set
// only on transitions leaving a task's current bucket in tasks.continue.
type TransitionSummary struct {
	From   string               `json:"from"`
	To     string               `json:"to"`
	Ready  *bool                `json:"ready,omitempty"`
	Guards []domain.GuardStatus `json:"guards,omitempty"`
}

type WorkflowInput struct {
	ProjectSelector
}

type WorkflowResponse struct {
	Project  ProjectSummary  `json:"project"`
	Workflow WorkflowSummary `json:"workflow"`
}

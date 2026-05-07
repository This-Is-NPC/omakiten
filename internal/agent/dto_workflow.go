package agent

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

func workflowSummary(workflow domain.Workflow) WorkflowSummary {
	out := WorkflowSummary{Key: workflow.Key, Name: workflow.Name}
	for _, bucket := range workflow.Buckets {
		out.Buckets = append(out.Buckets, BucketSummary{Key: bucket.Key, Name: bucket.Name, Position: bucket.Position})
	}
	for _, transition := range workflow.Transitions {
		out.Transitions = append(out.Transitions, TransitionSummary{From: transition.FromBucketKey, To: transition.ToBucketKey})
	}
	return out
}

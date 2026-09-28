package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func workflowSummary(workflow domain.Workflow) contract.WorkflowSummary {
	out := contract.WorkflowSummary{Key: workflow.Key, Name: workflow.Name}
	for _, bucket := range workflow.Buckets {
		out.Buckets = append(out.Buckets, contract.BucketSummary{Key: bucket.Key, Name: bucket.Name, Position: bucket.Position})
	}
	for _, transition := range workflow.Transitions {
		out.Transitions = append(out.Transitions, contract.TransitionSummary{From: transition.FromBucketKey, To: transition.ToBucketKey})
	}
	return out
}

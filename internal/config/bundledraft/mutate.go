package bundledraft

import (
	"fmt"

	"omakiten/internal/config"
)

func activeWorkflowPtr(bundle *config.Bundle) *config.Workflow {
	if bundle == nil || len(bundle.Workflows) == 0 {
		return nil
	}
	for i := range bundle.Workflows {
		if bundle.Workflows[i].Key == bundle.Config.Workflow.Active {
			return &bundle.Workflows[i]
		}
	}
	return &bundle.Workflows[0]
}

func activeWorkflowBucket(bundle *config.Bundle, bucketID int) *config.Bucket {
	workflow := activeWorkflowPtr(bundle)
	if workflow == nil {
		return nil
	}
	return workflowBucket(workflow, bucketID)
}

func workflowBucket(workflow *config.Workflow, bucketID int) *config.Bucket {
	for i := range workflow.Buckets {
		if workflow.Buckets[i].ID == bucketID {
			return &workflow.Buckets[i]
		}
	}
	return nil
}

func normalizeBucketPositions(buckets []config.Bucket) {
	ordered := OrderedBuckets(buckets)
	positions := map[int]int{}
	for i, bucket := range ordered {
		positions[bucket.ID] = i + 1
	}
	for i := range buckets {
		buckets[i].Position = positions[buckets[i].ID]
	}
}

func updateBucketKeyReferences(bundle *config.Bundle, workflow *config.Workflow, oldKey, newKey string) {
	for i := range workflow.Transitions {
		updateGuardBucketKeys(workflow.Transitions[i].Guards, oldKey, newKey)
	}
	updateGuardBucketKeys(workflow.Operations.Archive.Guards, oldKey, newKey)
	updateGuardBucketKeys(workflow.Operations.Delete.Guards, oldKey, newKey)
	updateGuardBucketKeys(workflow.Operations.Unarchive.Guards, oldKey, newKey)
	for i, bucket := range bundle.Config.Views.Table.Filter.Bucket {
		if bucket == oldKey {
			bundle.Config.Views.Table.Filter.Bucket[i] = newKey
		}
	}
}

func updateGuardBucketKeys(guards []config.TransitionGuard, oldKey, newKey string) {
	for i := range guards {
		if guards[i].Type != "blockers_in" {
			continue
		}
		for j, bucket := range guards[i].Buckets {
			if bucket == oldKey {
				guards[i].Buckets[j] = newKey
			}
		}
	}
}

func removeBucketTransitions(transitions []config.Transition, bucketID int) []config.Transition {
	out := transitions[:0]
	for _, transition := range transitions {
		if transition.From == bucketID || transition.To == bucketID {
			continue
		}
		out = append(out, transition)
	}
	return out
}

func removeBucketGuardReferences(workflow *config.Workflow, key string, text Text) error {
	if key == "" {
		return nil
	}
	for i := range workflow.Transitions {
		if err := removeGuardBucketReference(workflow.Transitions[i].Guards, key, text); err != nil {
			return err
		}
	}
	for _, guards := range [][]config.TransitionGuard{
		workflow.Operations.Archive.Guards,
		workflow.Operations.Delete.Guards,
		workflow.Operations.Unarchive.Guards} {
		if err := removeGuardBucketReference(guards, key, text); err != nil {
			return err
		}
	}
	return nil
}

func removeGuardBucketReference(guards []config.TransitionGuard, key string, text Text) error {
	for i := range guards {
		if guards[i].Type != "blockers_in" {
			continue
		}
		buckets := guards[i].Buckets[:0]
		removed := false
		for _, bucket := range guards[i].Buckets {
			if bucket == key {
				removed = true
				continue
			}
			buckets = append(buckets, bucket)
		}
		guards[i].Buckets = buckets
		if removed && len(guards[i].Buckets) == 0 {
			return fmt.Errorf("%s", Tr(text, "tui.studio.err.bucket_only_blockers_in_ref", "bucket %q is the only blockers_in bucket reference; delete is blocked", key))
		}
	}
	return nil
}

func setEntityPermissionBool(permission *config.EntityPermission, op BucketPermissionOp, allowed bool) {
	policy := &config.CommentOpPolicy{Allow: &allowed}
	switch op {
	case BucketPermissionCreate:
		permission.Create = policy
	case BucketPermissionEdit:
		permission.Edit = policy
	case BucketPermissionDelete:
		permission.Delete = policy
	}
}

func finalBucketKey(buckets []config.Bucket) string {
	if len(buckets) == 0 {
		return ""
	}
	final := buckets[0]
	for _, bucket := range buckets[1:] {
		if bucket.Position > final.Position || (bucket.Position == final.Position && bucket.Key > final.Key) {
			final = bucket
		}
	}
	return final.Key
}

func importedBlockReason(bundle config.Bundle, text Text) string {
	if len(bundle.SourcePaths) > 1 {
		return Tr(text, "tui.studio.diff.imported_readonly", "imported config blocks are read-only in Studio MVP")
	}
	return ""
}

func editWarnings(bundle config.Bundle, text Text) []string {
	warnings := make([]string, 0, len(bundle.Warnings)+1)
	for _, warning := range bundle.Warnings {
		warnings = append(warnings, warning.Message)
	}
	if reason := importedBlockReason(bundle, text); reason != "" {
		warnings = append(warnings, reason)
	}
	return warnings
}

package bundledraft

import (
	"reflect"
	"sort"
	"strings"

	"omakiten/internal/config"
)

// Diff summarises the candidate against the baseline as operator-facing lines.
// It never returns an empty slice: an unrecognised difference reports
// "Configuration changed" and an identical pair reports "No changes", so a
// caller can print element zero without a length check.
func Diff(original, candidate config.Bundle, text Text) []string {
	var out []string
	originalWorkflow, okOriginal := ActiveWorkflow(original)
	candidateWorkflow, okCandidate := ActiveWorkflow(candidate)
	if okOriginal && okCandidate {
		out = append(out, diffBuckets(originalWorkflow, candidateWorkflow, text)...)
		out = append(out, diffTransitions(originalWorkflow, candidateWorkflow, text)...)
		out = append(out, diffGuards(originalWorkflow, candidateWorkflow, text)...)
	}
	if !reflect.DeepEqual(original.Config.Views.Table.Filter.Bucket, candidate.Config.Views.Table.Filter.Bucket) {
		out = append(out, Tr(text, "tui.studio.diff.table_bucket_filter", "Table bucket filter changed: %s -> %s",
			formatStringList(original.Config.Views.Table.Filter.Bucket, text),
			formatStringList(candidate.Config.Views.Table.Filter.Bucket, text)))
	}
	out = append(out, diffMCPCommands(original.MCPCommands, candidate.MCPCommands, text)...)
	if len(out) == 0 {
		if !reflect.DeepEqual(original, candidate) {
			return []string{Tr(text, "tui.studio.diff.configuration_changed", "Configuration changed")}
		}
		return []string{Tr(text, "tui.studio.diff.no_changes", "No changes")}
	}
	return out
}

func diffBuckets(original, candidate config.Workflow, text Text) []string {
	var out []string
	originalByID := bucketsByID(original.Buckets)
	candidateByID := bucketsByID(candidate.Buckets)
	for _, bucket := range candidate.Buckets {
		before, ok := originalByID[bucket.ID]
		if !ok {
			out = append(out, Tr(text, "tui.studio.diff.bucket_added", "Bucket added: %s", bucket.Key))
			continue
		}
		if before.Key != bucket.Key {
			out = append(out, Tr(text, "tui.studio.diff.bucket_key_changed", "Bucket key changed: %s -> %s", before.Key, bucket.Key))
		}
		if before.Name != bucket.Name {
			out = append(out, Tr(text, "tui.studio.diff.bucket_renamed", "Bucket renamed: %s -> %s", before.Name, bucket.Name))
		}
		if !reflect.DeepEqual(before.Permissions, bucket.Permissions) {
			out = append(out, Tr(text, "tui.studio.diff.bucket_permissions_changed", "Bucket permissions changed: %s", bucket.Key))
		}
	}
	for _, bucket := range original.Buckets {
		if _, ok := candidateByID[bucket.ID]; !ok {
			out = append(out, Tr(text, "tui.studio.diff.bucket_removed", "Bucket removed: %s", bucket.Key))
		}
	}
	if bucketOrder(original.Buckets) != bucketOrder(candidate.Buckets) {
		out = append(out, Tr(text, "tui.studio.diff.buckets_reordered", "Buckets reordered"))
	}
	return out
}

func diffTransitions(original, candidate config.Workflow, text Text) []string {
	var out []string
	originalSet := TransitionSet(original.Transitions)
	candidateSet := TransitionSet(candidate.Transitions)
	names := bucketNamesByID(candidate.Buckets)
	for _, transition := range candidate.Transitions {
		key := TransitionKey(transition)
		if _, ok := originalSet[key]; !ok {
			out = append(out, Tr(text, "tui.studio.diff.transition_added", "Transition added: %s -> %s", names[transition.From], names[transition.To]))
		}
	}
	names = bucketNamesByID(original.Buckets)
	for _, transition := range original.Transitions {
		key := TransitionKey(transition)
		if _, ok := candidateSet[key]; !ok {
			out = append(out, Tr(text, "tui.studio.diff.transition_removed", "Transition removed: %s -> %s", names[transition.From], names[transition.To]))
			if len(transition.Guards) > 0 {
				out = append(out, Tr(text, "tui.studio.diff.transition_guards_removed", "Transition guards removed: %s -> %s (%d)", names[transition.From], names[transition.To], len(transition.Guards)))
			}
		}
	}
	return out
}

func diffGuards(original, candidate config.Workflow, text Text) []string {
	var out []string
	candidateByTransition := TransitionSet(candidate.Transitions)
	originalNames := bucketNamesByID(original.Buckets)
	for _, transition := range original.Transitions {
		candidateTransition, ok := candidateByTransition[TransitionKey(transition)]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(transition.Guards, candidateTransition.Guards) {
			out = append(out, Tr(text, "tui.studio.diff.guards_changed", "Guards changed: %s -> %s (%s -> %s)",
				originalNames[transition.From], originalNames[transition.To],
				formatGuards(transition.Guards, text), formatGuards(candidateTransition.Guards, text)))
		}
	}
	operations := []struct {
		name                string
		original, candidate []config.TransitionGuard
	}{
		{"archive", original.Operations.Archive.Guards, candidate.Operations.Archive.Guards},
		{"delete", original.Operations.Delete.Guards, candidate.Operations.Delete.Guards},
		{"unarchive", original.Operations.Unarchive.Guards, candidate.Operations.Unarchive.Guards}}
	for _, operation := range operations {
		if !reflect.DeepEqual(operation.original, operation.candidate) {
			out = append(out, Tr(text, "tui.studio.diff.operation_guards_changed", "Operation guards changed: %s (%s -> %s)",
				operation.name, formatGuards(operation.original, text), formatGuards(operation.candidate, text)))
		}
	}
	return out
}

func formatGuards(guards []config.TransitionGuard, text Text) string {
	if len(guards) == 0 {
		return Tr(text, "tui.studio.diff.none", "none")
	}
	values := make([]string, len(guards))
	for i, guard := range guards {
		details := make([]string, 0, 4)
		if len(guard.Buckets) > 0 {
			details = append(details, Tr(text, "tui.studio.diff.guard_detail.buckets", "buckets=%s", strings.Join(guard.Buckets, ",")))
		}
		if guard.Count != 0 {
			details = append(details, Tr(text, "tui.studio.diff.guard_detail.count", "count=%d", guard.Count))
		}
		if guard.Tag != "" {
			details = append(details, Tr(text, "tui.studio.diff.guard_detail.tag", "tag=%s", guard.Tag))
		}
		if guard.Hint != "" {
			details = append(details, Tr(text, "tui.studio.diff.guard_detail.hint", "hint=%s", guard.Hint))
		}
		values[i] = guard.Type
		if len(details) > 0 {
			values[i] += "[" + strings.Join(details, ",") + "]"
		}
	}
	return strings.Join(values, "; ")
}

func formatStringList(values []string, text Text) string {
	if len(values) == 0 {
		return Tr(text, "tui.studio.diff.none", "none")
	}
	return strings.Join(values, ", ")
}

func diffMCPCommands(original, candidate map[string]config.MCPCommandSpec, text Text) []string {
	if reflect.DeepEqual(original, candidate) {
		return nil
	}
	keys := map[string]struct{}{}
	for key := range original {
		keys[key] = struct{}{}
	}
	for key := range candidate {
		keys[key] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	out := make([]string, 0, len(sorted))
	for _, key := range sorted {
		before, hadBefore := original[key]
		after, hasAfter := candidate[key]
		switch {
		case !hadBefore:
			out = append(out, Tr(text, "tui.studio.diff.command_binding_added", "Command binding added: %s", key))
		case !hasAfter:
			out = append(out, Tr(text, "tui.studio.diff.command_binding_removed", "Command binding removed: %s", key))
		case !reflect.DeepEqual(before, after):
			out = append(out, Tr(text, "tui.studio.diff.command_binding_changed", "Command binding changed: %s", key))
		}
	}
	return out
}

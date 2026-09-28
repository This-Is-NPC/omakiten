package settingsprojection

import (
	"sort"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// EffectiveSection groups effective configuration rows in the display order
// shared by settings consumers.
type EffectiveSection struct {
	Name   string
	Tuples []config.EffectiveTuple
}

var effectiveOrder = []string{"theme", "tricks", "priorities", "severities", "views", "context", "tui", "output", "search", "solutions", "mcp", "events", "sqlite", "backup", "hooks"}

var hiddenSections = map[string]bool{"languages": true, "workflow": true, "template_defaults": true, "tag_synonyms": true}

// EffectiveSections groups effective configuration tuples, omitting internal
// sections and preserving the stable configured section order.
func EffectiveSections(snapshot *config.Snapshot) []EffectiveSection {
	if snapshot == nil {
		return nil
	}
	grouped := groupTuples(snapshot.EffectiveTuples())
	return sectionsFrom(grouped, orderedSectionNames(snapshot.EffectiveSectionKeys()))
}

func groupTuples(tuples []config.EffectiveTuple) map[string][]config.EffectiveTuple {
	grouped := map[string][]config.EffectiveTuple{}
	for _, tuple := range tuples {
		grouped[tuple.Section] = append(grouped[tuple.Section], tuple)
	}
	return grouped
}

func orderedSectionNames(keys []string) []string {
	present := make(map[string]bool, len(keys))
	for _, section := range keys {
		present[section] = true
	}
	names := make([]string, 0, len(present))
	for _, section := range effectiveOrder {
		if present[section] && !hiddenSections[section] {
			names = append(names, section)
		}
		delete(present, section)
	}
	for _, section := range keys {
		if present[section] && !hiddenSections[section] {
			names = append(names, section)
		}
	}
	return names
}

func sectionsFrom(grouped map[string][]config.EffectiveTuple, names []string) []EffectiveSection {
	sections := make([]EffectiveSection, 0, len(names))
	for _, name := range names {
		if len(grouped[name]) > 0 {
			sections = append(sections, EffectiveSection{Name: name, Tuples: grouped[name]})
		}
	}
	return sections
}

// OrderedBuckets returns a copy sorted by configured position.
func OrderedBuckets(in []domain.Bucket) []domain.Bucket {
	out := append([]domain.Bucket(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// TransitionIndex returns the declared directed workflow edges.
func TransitionIndex(in []domain.WorkflowTransition) map[[2]string]bool {
	out := make(map[[2]string]bool, len(in))
	for _, transition := range in {
		out[[2]string{transition.FromBucketKey, transition.ToBucketKey}] = true
	}
	return out
}

// GuardMatricesEqual reports whether two snapshots render the same workflow
// guard matrix, independent of declaration order inside guards or transitions.
func GuardMatricesEqual(a, b *config.Snapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	wa, wb := a.Workflow(), b.Workflow()
	buckets := OrderedBuckets(wa.Buckets)
	if !bucketsEqual(buckets, OrderedBuckets(wb.Buckets)) || !TransitionsEqual(wa.Transitions, wb.Transitions) {
		return false
	}
	for _, from := range buckets {
		for _, to := range buckets {
			if from.ID == to.ID {
				continue
			}
			if !GuardsEqual(NormalizeGuards(a.Guards(from.ID, to.ID)), NormalizeGuards(b.Guards(from.ID, to.ID))) {
				return false
			}
		}
	}
	return true
}

func bucketsEqual(a, b []domain.Bucket) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Key != b[i].Key || a[i].Position != b[i].Position {
			return false
		}
	}
	return true
}

// TransitionsEqual compares directed transition sets without relying on
// declaration order.
func TransitionsEqual(a, b []domain.WorkflowTransition) bool {
	if len(a) != len(b) {
		return false
	}
	indexed := TransitionIndex(a)
	for _, transition := range b {
		if !indexed[[2]string{transition.FromBucketKey, transition.ToBucketKey}] {
			return false
		}
	}
	return true
}

// NormalizeGuards returns a sorted, deep-copied guard slice for equality.
func NormalizeGuards(in []domain.TransitionGuard) []domain.TransitionGuard {
	out := append([]domain.TransitionGuard(nil), in...)
	for i := range out {
		out[i].Buckets = append([]string(nil), out[i].Buckets...)
		sort.Strings(out[i].Buckets)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Tag != b.Tag {
			return a.Tag < b.Tag
		}
		if a.Count != b.Count {
			return a.Count < b.Count
		}
		if a.Hint != b.Hint {
			return a.Hint < b.Hint
		}
		return strings.Join(a.Buckets, ",") < strings.Join(b.Buckets, ",")
	})
	return out
}

// GuardsEqual compares normalized guard slices.
func GuardsEqual(a, b []domain.TransitionGuard) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Tag != b[i].Tag || a[i].Count != b[i].Count || a[i].Hint != b[i].Hint || strings.Join(a[i].Buckets, "\x00") != strings.Join(b[i].Buckets, "\x00") {
			return false
		}
	}
	return true
}

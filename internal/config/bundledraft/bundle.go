package bundledraft

import (
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/config"
)

// ActiveWorkflow returns the workflow the bundle declares active, by value.
// The second result is false when the bundle carries no workflow at all or no
// workflow whose key matches `config.workflow.active`.
func ActiveWorkflow(bundle config.Bundle) (config.Workflow, bool) {
	if len(bundle.Workflows) == 0 {
		return config.Workflow{}, false
	}
	for _, workflow := range bundle.Workflows {
		if workflow.Key == bundle.Config.Workflow.Active {
			return workflow, true
		}
	}
	return config.Workflow{}, false
}

// OrderedBuckets copies the buckets and sorts them by declared position, id
// breaking a tie. Callers get board order without mutating the bundle.
func OrderedBuckets(in []config.Bucket) []config.Bucket {
	out := append([]config.Bucket(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position == out[j].Position {
			return out[i].ID < out[j].ID
		}
		return out[i].Position < out[j].Position
	})
	return out
}

// TransitionSet indexes the transitions by their "from:to" key so a lookup
// against another workflow's transitions is a map hit rather than a scan.
func TransitionSet(transitions []config.Transition) map[string]config.Transition {
	out := make(map[string]config.Transition, len(transitions))
	for _, transition := range transitions {
		out[TransitionKey(transition)] = transition
	}
	return out
}

// TransitionKey is the "from:to" identity of one transition.
func TransitionKey(transition config.Transition) string {
	return fmt.Sprintf("%d:%d", transition.From, transition.To)
}

// LoadedSkills prefers the full on-disk catalog over the wired subset, which is
// what the validator has to see to resolve a reference the bundle only names.
func LoadedSkills(bundle config.Bundle) []config.Skill {
	if bundle.AllSkills != nil {
		return bundle.AllSkills
	}
	return bundle.Skills
}

// LoadedLaws is [LoadedSkills] for laws.
func LoadedLaws(bundle config.Bundle) []config.Law {
	if bundle.AllLaws != nil {
		return bundle.AllLaws
	}
	return bundle.Laws
}

// LoadedPersonas is [LoadedSkills] for personas.
func LoadedPersonas(bundle config.Bundle) []config.Persona {
	if bundle.AllPersonas != nil {
		return bundle.AllPersonas
	}
	return bundle.Personas
}

// LoadedTemplates is [LoadedSkills] for task templates.
func LoadedTemplates(bundle config.Bundle) []config.TaskTemplate {
	if bundle.AllTemplates != nil {
		return bundle.AllTemplates
	}
	return bundle.Templates
}

func bucketsByID(buckets []config.Bucket) map[int]config.Bucket {
	out := make(map[int]config.Bucket, len(buckets))
	for _, bucket := range buckets {
		out[bucket.ID] = bucket
	}
	return out
}

func bucketNamesByID(buckets []config.Bucket) map[int]string {
	out := make(map[int]string, len(buckets))
	for _, bucket := range buckets {
		out[bucket.ID] = bucket.Key
	}
	return out
}

func bucketOrder(buckets []config.Bucket) string {
	parts := make([]string, len(buckets))
	ordered := append([]config.Bucket(nil), buckets...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Position == ordered[j].Position {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Position < ordered[j].Position
	})
	for i, bucket := range ordered {
		parts[i] = fmt.Sprintf("%d", bucket.ID)
	}
	return strings.Join(parts, ",")
}

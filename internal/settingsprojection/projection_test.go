package settingsprojection

import (
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestGuardMatricesEqualIgnoresDeclarationOrder(t *testing.T) {
	a := snapshot([]config.TransitionGuard{{Type: "comments_tagged", Tag: "reviewed", Count: 1}, {Type: "tests_passing"}})
	b := snapshot([]config.TransitionGuard{{Type: "tests_passing"}, {Type: "comments_tagged", Tag: "reviewed", Count: 1}})
	if !GuardMatricesEqual(a, b) {
		t.Fatal("equivalent guard declarations should compare equal")
	}
	b = snapshot([]config.TransitionGuard{{Type: "comments_tagged", Tag: "changed"}})
	if GuardMatricesEqual(a, b) {
		t.Fatal("changed guard declarations should compare different")
	}
}

func TestTransitionIndexAndBucketOrder(t *testing.T) {
	buckets := OrderedBuckets([]domain.Bucket{{ID: 2, Key: "dev", Position: 2}, {ID: 1, Key: "backlog", Position: 1}})
	if buckets[0].Key != "backlog" {
		t.Fatalf("bucket order = %#v", buckets)
	}
	index := TransitionIndex([]domain.WorkflowTransition{{FromBucketKey: "backlog", ToBucketKey: "dev"}})
	if !index[[2]string{"backlog", "dev"}] {
		t.Fatal("transition index omitted declared edge")
	}
}

func TestEffectiveSectionsHideInternalSections(t *testing.T) {
	sections := EffectiveSections(snapshot(nil))
	if len(sections) == 0 {
		t.Fatal("effective sections should expose configured settings")
	}
	for _, section := range sections {
		if hiddenSections[section.Name] {
			t.Fatalf("internal section %q leaked into effective settings", section.Name)
		}
	}
}

func snapshot(guards []config.TransitionGuard) *config.Snapshot {
	return config.BuildSnapshot(config.Bundle{
		Kit: config.Kit{Key: "root"}, Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{ID: 1, Key: "root", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Position: 1}, {ID: 2, Key: "done", Position: 2}}, Transitions: []config.Transition{{From: 1, To: 2, Guards: guards}}}},
	})
}

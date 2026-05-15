package config

import (
	"reflect"
	"testing"

	"omakiten/internal/domain"
)

// TestSnapshotIsImmutableUnderBundleMutation pins the architectural
// invariant: once BuildSnapshot returns, the source Bundle may be
// mutated without disturbing the snapshot's view. The pre-Phase-2-bis
// implementation (config.InMemoryProviders) held a Bundle pointer
// internally and Swap'd it — that broke per-project isolation as soon
// as N projects shared a Store.
func TestSnapshotIsImmutableUnderBundleMutation(t *testing.T) {
	bundle := newTwoBucketBundle("alpha", "beta")
	snap := BuildSnapshot(bundle)

	// Mutate the source bundle after the snapshot is taken.
	bundle.Workflows[0].Buckets[0].Key = "MUTATED"
	bundle.Personas = append(bundle.Personas, Persona{Slug: "rogue"})
	bundle.Skills = append(bundle.Skills, Skill{Slug: "rogue"})

	if _, ok := snap.BucketByKey("alpha"); !ok {
		t.Fatal("Snapshot.BucketByKey(alpha) lost the original bucket after Bundle mutation")
	}
	if _, ok := snap.BucketByKey("MUTATED"); ok {
		t.Fatal("Snapshot picked up the post-Build mutation; expected immutable view")
	}
	if _, ok := snap.PersonaBySlug("rogue"); ok {
		t.Fatal("Snapshot picked up a post-Build persona append; expected immutable view")
	}
	if _, ok := snap.SkillBySlug("rogue"); ok {
		t.Fatal("Snapshot picked up a post-Build skill append; expected immutable view")
	}
}

// TestSnapshotReturnsFreshSlices pins the second leg of immutability:
// callers that mutate the slice returned by Workflow / Personas /
// Skills / Laws / Templates must not corrupt subsequent reads. A
// previous implementation (the provider snapshot) shared the underlying
// array; mutating the returned slice header leaked into the next
// reader.
func TestSnapshotReturnsFreshSlices(t *testing.T) {
	bundle := newTwoBucketBundle("alpha", "beta")
	bundle.Personas = []Persona{{Slug: "engineer", Name: "Engineer"}}
	snap := BuildSnapshot(bundle)

	first := snap.Personas()
	if len(first) == 0 {
		t.Fatal("expected at least one persona")
	}
	first[0] = Persona{Slug: "OVERWRITTEN"}

	second := snap.Personas()
	if second[0].Slug != "engineer" {
		t.Fatalf("mutation of caller-returned slice leaked into snapshot: got %q, want %q", second[0].Slug, "engineer")
	}
}

// TestSnapshotPerBundleIsolation pins the per-project intent. Two
// distinct bundles produce two distinct snapshots whose lookups do not
// cross. This is the property the cache layer leans on when handing a
// project its own Snapshot pointer.
func TestSnapshotPerBundleIsolation(t *testing.T) {
	a := newTwoBucketBundle("planning", "review")
	b := newTwoBucketBundle("todo", "doing")

	snapA := BuildSnapshot(a)
	snapB := BuildSnapshot(b)

	if _, ok := snapA.BucketByKey("todo"); ok {
		t.Fatal("snapshot A leaked bucket from bundle B (todo)")
	}
	if _, ok := snapB.BucketByKey("planning"); ok {
		t.Fatal("snapshot B leaked bucket from bundle A (planning)")
	}

	wantA := []string{"planning", "review"}
	wantB := []string{"todo", "doing"}
	if got := bucketKeys(snapA.Workflow()); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("snap A buckets = %v, want %v", got, wantA)
	}
	if got := bucketKeys(snapB.Workflow()); !reflect.DeepEqual(got, wantB) {
		t.Fatalf("snap B buckets = %v, want %v", got, wantB)
	}
}

func newTwoBucketBundle(firstKey, secondKey string) Bundle {
	return Bundle{
		Workflows: []Workflow{{
			ID:   1,
			Key:  "test",
			Name: "Test Workflow",
			Buckets: []Bucket{
				{ID: 1, Key: firstKey, Name: firstKey, Position: 1},
				{ID: 2, Key: secondKey, Name: secondKey, Position: 2},
			},
			Transitions: []Transition{
				{From: 1, To: 2},
			},
		}},
		Config: Settings{
			Workflow: WorkflowSettings{Active: "test"},
		},
	}
}

func bucketKeys(wf domain.Workflow) []string {
	out := make([]string, 0, len(wf.Buckets))
	for _, b := range wf.Buckets {
		out = append(out, b.Key)
	}
	return out
}

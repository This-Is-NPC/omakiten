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

// TestSnapshotExposesActiveTheme pins the contract introduced by the
// Phase 2-bis Theme-via-Snapshot closure: BuildSnapshot copies the
// resolved Theme from the Bundle so the TUI hot-reload + first-boot
// paths read tokens through `snap.Theme()` instead of re-opening the
// themes/<slug>.yaml file on every Reload. Two snapshots built from
// distinct bundles carry distinct themes; a bundle that ships an empty
// ActiveTheme produces a zero-Theme on the snapshot (callers that need
// a live theme detect that and surface ErrConfigInvalid themselves).
func TestSnapshotExposesActiveTheme(t *testing.T) {
	bundle := newTwoBucketBundle("alpha", "beta")
	bundle.ActiveTheme = Theme{
		Version: 1,
		Key:     "omacon",
		Name:    "Omacon",
		Colors:  map[string]string{"accent": "#ff5fa2"},
	}
	snap := BuildSnapshot(bundle)

	got := snap.Theme()
	if got.Key != "omacon" || got.Name != "Omacon" {
		t.Fatalf("snap.Theme() = %+v, want key=omacon name=Omacon", got)
	}
	if got.Colors["accent"] != "#ff5fa2" {
		t.Fatalf("snap.Theme().Colors[accent] = %q, want #ff5fa2", got.Colors["accent"])
	}

	// Mutating the bundle after BuildSnapshot must not leak into the
	// snapshot (matches the immutability contract the rest of the
	// snapshot fields already honor).
	bundle.ActiveTheme.Colors["accent"] = "MUTATED"
	if snap.Theme().Colors["accent"] == "MUTATED" {
		t.Fatal("snapshot leaked post-Build mutation of Bundle.ActiveTheme.Colors")
	}

	// Empty ActiveTheme yields zero-Theme on the accessor.
	emptyBundle := newTwoBucketBundle("alpha", "beta")
	emptySnap := BuildSnapshot(emptyBundle)
	if emptySnap.Theme().Name != "" {
		t.Fatalf("zero-ActiveTheme: snap.Theme().Name = %q, want \"\"", emptySnap.Theme().Name)
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

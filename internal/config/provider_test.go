package config

import (
	"sync"
	"sync/atomic"
	"testing"
)

// fixtureBundle returns a small but representative bundle covering every
// provider surface. Used by every provider test below so the assertions
// stay anchored to a single source of truth.
func fixtureBundle() Bundle {
	t := true
	return Bundle{
		Version: 1,
		Config: Settings{
			Workflow: WorkflowSettings{Active: "omakase"},
		},
		Workflows: []Workflow{
			{
				ID:   1,
				Key:  "omakase",
				Name: "Omakase",
				Buckets: []Bucket{
					{ID: 10, Key: "backlog", Name: "Backlog", Position: 1},
					{ID: 20, Key: "dev", Name: "Development", Position: 2},
					{
						ID:       30,
						Key:      "review",
						Name:     "Review",
						Position: 3,
						Permissions: &BucketPermissions{
							Task: &EntityPermission{Edit: &t},
						},
					},
					{ID: 40, Key: "done", Name: "Done", Position: 4},
				},
				Transitions: []Transition{
					{From: 10, To: 20},
					{From: 20, To: 30, Guards: []TransitionGuard{
						{Type: "comments_min", Count: 1, Hint: "Add a comment first"},
					}},
					{From: 30, To: 40, Guards: []TransitionGuard{
						{Type: "comments_tagged", Tag: "documentation", Count: 1},
					}},
				},
				Operations: WorkflowOperations{
					Archive: OperationPolicy{Guards: []TransitionGuard{
						{Type: "blockers_in", Buckets: []string{"dev"}},
					}},
				},
			},
			{
				ID:   2,
				Key:  "flat",
				Name: "Flat",
				Buckets: []Bucket{
					{ID: 100, Key: "todo", Name: "Todo", Position: 1},
				},
			},
		},
		Personas: []Persona{
			{Slug: "engineer", Name: "Engineer", Skills: []string{"go"}},
			{Slug: "analyst", Name: "Analyst"},
		},
		Skills: []Skill{
			{Slug: "go", Name: "Go"},
			{Slug: "sql", Name: "SQL"},
		},
		Laws: []Law{
			{Slug: "scope", Severity: "error", Body: "Stay in scope"},
		},
		Templates: []TaskTemplate{
			{Slug: "pull-request", Name: "PR", Default: "pr"},
			{Slug: "pr-project", Name: "Project PR", Default: "pr", ProjectSlug: "omakiten"},
		},
		Notifications: map[string]Notification{
			"info": {},
		},
		MCPCommands: map[string]MCPCommandSpec{
			"global":         {Laws: []string{"scope"}},
			"okt-implement":  {Persona: "engineer"},
		},
	}
}

func TestProvidersWorkflowLookups(t *testing.T) {
	p := NewInMemoryProviders(fixtureBundle())

	wf := p.Workflow()
	if wf.Key != "omakase" {
		t.Fatalf("active workflow: got %q want %q", wf.Key, "omakase")
	}
	if len(wf.Buckets) != 4 {
		t.Fatalf("buckets: got %d want 4", len(wf.Buckets))
	}
	if len(wf.Transitions) != 3 {
		t.Fatalf("transitions: got %d want 3", len(wf.Transitions))
	}

	if _, ok := p.BucketByID(20); !ok {
		t.Fatal("BucketByID(20) missing")
	}
	if b, ok := p.BucketByKey("review"); !ok || b.ID != 30 || b.Permissions == nil {
		t.Fatalf("BucketByKey(review): got id=%d perms=%v ok=%v", b.ID, b.Permissions, ok)
	}
	if _, ok := p.BucketByID(999); ok {
		t.Fatal("BucketByID(999) should miss")
	}

	if !p.TransitionAllowed(20, 30) {
		t.Fatal("TransitionAllowed(dev→review) should be true")
	}
	if p.TransitionAllowed(40, 10) {
		t.Fatal("TransitionAllowed(done→backlog) should be false (not declared)")
	}

	guards := p.Guards(20, 30)
	if len(guards) != 1 || guards[0].Type != "comments_min" || guards[0].Count != 1 {
		t.Fatalf("Guards(dev→review): got %+v", guards)
	}
	if got := p.Guards(10, 20); len(got) != 0 {
		t.Fatalf("Guards(backlog→dev): want empty got %+v", got)
	}
	if got := p.Guards(999, 999); got != nil {
		t.Fatalf("Guards on missing pair: want nil got %+v", got)
	}

	if !p.IsFinalBucket(40) {
		t.Fatal("IsFinalBucket(done) should be true")
	}
	if p.IsFinalBucket(30) {
		t.Fatal("IsFinalBucket(review) should be false")
	}

	ops := p.Operations()
	if len(ops.Archive.Guards) != 1 || ops.Archive.Guards[0].Type != "blockers_in" {
		t.Fatalf("Operations.Archive guards: got %+v", ops.Archive.Guards)
	}
}

func TestProvidersWorkflowFallbackWhenActiveSettingMissing(t *testing.T) {
	bundle := fixtureBundle()
	bundle.Config.Workflow.Active = ""
	p := NewInMemoryProviders(bundle)
	if p.Workflow().Key != "omakase" {
		t.Fatalf("unset active should fall back to first declared workflow; got %q", p.Workflow().Key)
	}

	bundle.Config.Workflow.Active = "nonexistent"
	p2 := NewInMemoryProviders(bundle)
	if p2.Workflow().Key != "omakase" {
		t.Fatalf("unknown active should fall back to first declared; got %q", p2.Workflow().Key)
	}
}

func TestProvidersWorkflowEmptyBundle(t *testing.T) {
	p := NewInMemoryProviders(Bundle{})
	if wf := p.Workflow(); wf.Key != "" {
		t.Fatalf("empty bundle: want zero workflow, got %+v", wf)
	}
	if _, ok := p.BucketByID(1); ok {
		t.Fatal("empty bundle: BucketByID should miss")
	}
	if p.TransitionAllowed(1, 2) {
		t.Fatal("empty bundle: no transitions should be allowed")
	}
	if p.IsFinalBucket(1) {
		t.Fatal("empty bundle: nothing is final")
	}
}

func TestProvidersEntityLookups(t *testing.T) {
	p := NewInMemoryProviders(fixtureBundle())

	if got := p.Personas(); len(got) != 2 {
		t.Fatalf("Personas len: got %d want 2", len(got))
	}
	if persona, ok := p.PersonaBySlug("engineer"); !ok || persona.Name != "Engineer" {
		t.Fatalf("PersonaBySlug(engineer): got %+v ok=%v", persona, ok)
	}
	if _, ok := p.PersonaBySlug("ghost"); ok {
		t.Fatal("PersonaBySlug(ghost) should miss")
	}

	if got := p.Skills(); len(got) != 2 {
		t.Fatalf("Skills len: got %d", len(got))
	}
	if skill, ok := p.SkillBySlug("go"); !ok || skill.Name != "Go" {
		t.Fatalf("SkillBySlug(go): got %+v ok=%v", skill, ok)
	}

	if law, ok := p.LawBySlug("scope"); !ok || law.Severity != "error" {
		t.Fatalf("LawBySlug(scope): got %+v ok=%v", law, ok)
	}

	if _, ok := p.NotificationBySlug("info"); !ok {
		t.Fatal("NotificationBySlug(info) should hit")
	}
	if got := p.Notifications(); len(got) != 1 {
		t.Fatalf("Notifications len: got %d", len(got))
	}

	if spec, ok := p.MCPCommandByKey("okt-implement"); !ok || spec.Persona != "engineer" {
		t.Fatalf("MCPCommandByKey(okt-implement): got %+v ok=%v", spec, ok)
	}
	if got := p.MCPCommands(); len(got) != 2 {
		t.Fatalf("MCPCommands len: got %d", len(got))
	}
}

func TestProvidersTemplateActiveDefaultPrecedence(t *testing.T) {
	p := NewInMemoryProviders(fixtureBundle())

	// project-scoped wins over global
	t1, ok := p.ActiveDefault("pr", "omakiten")
	if !ok || t1.Slug != "pr-project" {
		t.Fatalf("project-scoped default: got %+v ok=%v", t1, ok)
	}

	// falls back to global when project does not declare one
	t2, ok := p.ActiveDefault("pr", "other-project")
	if !ok || t2.Slug != "pull-request" {
		t.Fatalf("fallback to global: got %+v ok=%v", t2, ok)
	}

	// global lookup without a project slug works the same
	t3, ok := p.ActiveDefault("pr", "")
	if !ok || t3.Slug != "pull-request" {
		t.Fatalf("global default: got %+v ok=%v", t3, ok)
	}

	// unknown kind misses
	if _, ok := p.ActiveDefault("ghost", ""); ok {
		t.Fatal("ActiveDefault(ghost) should miss")
	}
	if _, ok := p.ActiveDefault("", "omakiten"); ok {
		t.Fatal("ActiveDefault with empty kind should miss")
	}
}

func TestProvidersReturnedSlicesAreCopies(t *testing.T) {
	p := NewInMemoryProviders(fixtureBundle())

	// Mutating the returned slice must not affect the snapshot — otherwise
	// a reader could corrupt the bundle for every other reader.
	personas := p.Personas()
	personas[0].Name = "MUTATED"
	if again, _ := p.PersonaBySlug("engineer"); again.Name != "Engineer" {
		t.Fatalf("returned slice shared with snapshot: got %q after mutation", again.Name)
	}

	wf := p.Workflow()
	wf.Buckets[0].Name = "MUTATED"
	if again, _ := p.BucketByID(10); again.Name != "Backlog" {
		t.Fatalf("returned workflow shares bucket slice: got %q after mutation", again.Name)
	}
}

func TestProvidersSwapIsAtomicUnderConcurrency(t *testing.T) {
	// Goal: a Swap from bundle A to bundle B must never expose a half-built
	// mix. We assert this indirectly: while a writer flips between two
	// bundles whose workflow.Key differs, readers must only ever observe
	// one of the two values — not "" and not a stale value from a
	// different snapshot. Combined with `go test -race`, this surfaces
	// any non-atomic state transition.
	bundleA := fixtureBundle()
	bundleA.Workflows[0].Key = "alpha"
	bundleA.Config.Workflow.Active = "alpha"
	bundleB := fixtureBundle()
	bundleB.Workflows[0].Key = "bravo"
	bundleB.Config.Workflow.Active = "bravo"

	p := NewInMemoryProviders(bundleA)

	var stop atomic.Bool
	var wg sync.WaitGroup

	// writer flips between the two snapshots as fast as it can
	wg.Add(1)
	go func() {
		defer wg.Done()
		toggle := false
		for !stop.Load() {
			if toggle {
				p.Swap(&bundleA)
			} else {
				p.Swap(&bundleB)
			}
			toggle = !toggle
		}
	}()

	// readers must only ever see "alpha" or "bravo"
	const readers = 8
	const iterationsPerReader = 5_000
	var observed atomic.Int64
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterationsPerReader; j++ {
				key := p.Workflow().Key
				switch key {
				case "alpha", "bravo":
					observed.Add(1)
				default:
					t.Errorf("torn read: workflow.Key=%q", key)
					return
				}
			}
		}()
	}

	// give readers a moment to start before flipping the stop signal
	for observed.Load() < readers {
		// spin until at least one read per reader has happened
	}
	stop.Store(true)
	wg.Wait()

	if observed.Load() == 0 {
		t.Fatal("no successful reads observed; test did not exercise the swap")
	}
}

func TestProvidersSwapNilBundleProducesEmptySnapshot(t *testing.T) {
	p := NewInMemoryProviders(fixtureBundle())
	p.Swap(nil)
	if wf := p.Workflow(); wf.Key != "" {
		t.Fatalf("nil swap should empty the snapshot, got workflow %+v", wf)
	}
	if got := p.Personas(); len(got) != 0 {
		t.Fatalf("nil swap should clear personas, got %d", len(got))
	}
}

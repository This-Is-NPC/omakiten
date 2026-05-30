package agent

import (
	"strings"
	"testing"
)

// TestNoteCommandsTargetScopedComments pins the #386 repoint: the okt-note-*
// atomics (and the orchestrators that persist/read handoffs) now drive the
// scope-aware `comments.*` surface, since the `notes.*` entity was removed.
// No action body may still reference a `notes.` tool, and the note-family
// commands must name `comments.` so the prompts route the user to the live API.
func TestNoteCommandsTargetScopedComments(t *testing.T) {
	// Every command body must be free of the removed notes.* tool namespace.
	for _, slug := range CommandNames() {
		body := CommandActionFallback(slug)
		if strings.Contains(body, "notes.") {
			t.Errorf("command %q action still references removed notes.* tool:\n%s", slug, body)
		}
	}

	// The note-family + handoff-persisting commands must drive comments.*.
	mustReferenceComments := []string{
		"okt-note-free",
		"okt-note-recap",
		"okt-note-list",
		"okt-note-show",
		"okt-pause",
		"okt-start",
	}
	for _, slug := range mustReferenceComments {
		body := CommandActionFallback(slug)
		if body == "" {
			t.Fatalf("command %q has no action body", slug)
		}
		if !strings.Contains(body, "comments.") {
			t.Errorf("command %q action does not reference the comments.* surface:\n%s", slug, body)
		}
	}

	// Slugs stay put — the mcp_test note( group + command count depend on them.
	for _, slug := range []string{"okt-note-free", "okt-note-recap", "okt-note-list", "okt-note-show"} {
		if _, ok := DescribeCommand(slug); !ok {
			t.Fatalf("note slug %q was renamed/removed; mcp_test note group + count rely on it", slug)
		}
	}
}

// TestDescribeCommandTiers pins the three-tier classification + object
// namespacing the v2 command surface (#371) routes on. Bare slugs split into
// orchestrators (primary path) and system commands (talk to the tool);
// object-prefixed slugs (`okt-<object>-<verb>`) resolve as granular with the
// object + verb decoded.
func TestDescribeCommandTiers(t *testing.T) {
	cases := []struct {
		slug   string
		tier   CommandTier
		object string
		verb   string
		ok     bool
	}{
		// Bare alias for the primary path.
		{"okt", CommandTierOrchestrator, "", "", true},
		// Orchestrators — bare, primary, cross-object.
		{"okt-start", CommandTierOrchestrator, "", "start", true},
		{"okt-run", CommandTierOrchestrator, "", "run", true},
		{"okt-shape", CommandTierOrchestrator, "", "shape", true},
		{"okt-audit", CommandTierOrchestrator, "", "audit", true},
		{"okt-pause", CommandTierOrchestrator, "", "pause", true},
		// System — bare, talks to the tool not the project.
		{"okt-help", CommandTierSystem, "", "help", true},
		{"okt-config", CommandTierSystem, "", "config", true},
		{"okt-skill", CommandTierSystem, "", "skill", true},
		// Granular — object + verb.
		{"okt-task-implement", CommandTierGranular, "task", "implement", true},
		{"okt-task-self-review", CommandTierGranular, "task", "self-review", true},
		{"okt-plan-create", CommandTierGranular, "plan", "create", true},
		{"okt-project-resume", CommandTierGranular, "project", "resume", true},
		{"okt-note-free", CommandTierGranular, "note", "free", true},
		// Whitespace tolerance.
		{"  okt-task-implement  ", CommandTierGranular, "task", "implement", true},
		// Unknown / malformed.
		{"", "", "", "", false},
		{"task-implement", "", "", "", false},
		{"okt-", "", "", "", false},
		{"okt-task-", "", "", "", false},
	}
	for _, c := range cases {
		got, ok := DescribeCommand(c.slug)
		if ok != c.ok {
			t.Fatalf("DescribeCommand(%q) ok = %t, want %t", c.slug, ok, c.ok)
		}
		if !c.ok {
			continue
		}
		if got.Tier != c.tier {
			t.Errorf("DescribeCommand(%q).Tier = %q, want %q", c.slug, got.Tier, c.tier)
		}
		if got.Object != c.object {
			t.Errorf("DescribeCommand(%q).Object = %q, want %q", c.slug, got.Object, c.object)
		}
		if got.Verb != c.verb {
			t.Errorf("DescribeCommand(%q).Verb = %q, want %q", c.slug, got.Verb, c.verb)
		}
	}
}

// TestDescribeCommandUnknownBareSlug guards the default for an unrecognized
// bare verb: it is not a known orchestrator or system command, so resolution
// fails rather than silently mislabeling it.
func TestDescribeCommandUnknownBareSlug(t *testing.T) {
	if _, ok := DescribeCommand("okt-frobnicate"); ok {
		t.Fatal("DescribeCommand(okt-frobnicate) ok = true, want false for unknown bare verb")
	}
}

// TestCommandTierString covers the tier enum's human label used by help.
func TestCommandTierString(t *testing.T) {
	cases := map[CommandTier]string{
		CommandTierOrchestrator: "orchestrator",
		CommandTierSystem:       "system",
		CommandTierGranular:     "granular",
	}
	for tier, want := range cases {
		if string(tier) != want {
			t.Errorf("CommandTier %q string = %q, want %q", tier, string(tier), want)
		}
	}
}

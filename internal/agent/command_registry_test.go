package agent

import (
	"testing"
)

// TestNoteCommandSlugsRegistered pins that the okt-note-* slugs stay registered
// and tier-decodable — the mcp_test note group + command count depend on them.
// The operational prose contract (#386: bodies free of the removed notes.* tool,
// note-family commands drive the scope-aware comments.* surface) is now an
// entity-sourced property of the bound okt-<slug>-playbook skills, asserted
// against the rendered default kit by
// agentruntime.TestNoteCommandsTargetScopedComments — the Go layer no longer
// carries that prose to check here.
func TestNoteCommandSlugsRegistered(t *testing.T) {
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

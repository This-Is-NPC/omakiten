package agent

import "strings"

// commandTable is the ONE ordered source of truth for the `okt-*` MCP prompt
// surface. Each row is just a Slug — the operational Action prose and the
// prompts/list Description no longer live in Go. They are ENTITY-SOURCED: every
// command binds an `okt-<slug>-playbook` skill (see playbookSlugForCommand), and
// the resolver renders that skill's body as the command playbook and its
// frontmatter `description` as the prompts/list one-liner. Keeping the table a
// bare ordered slug list means CommandNames() simply projects it, the order IS
// the canonical prompts/list order (the REST-style handoff cycle), and the slug
// set + tier-decodability (DescribeCommand) stay intact regardless of where the
// prose lives.
//
// The bare `okt` and the explicit `okt-start` both resolve their playbook to
// `okt-start-playbook` (playbookSlugForCommand), so the bare shortcut renders
// the same smart-entry concierge body as the explicit command.
var commandTable = []string{
	"okt",
	"okt-help",
	"okt-start",
	"okt-shape",
	"okt-run",
	"okt-task-imagine",
	"okt-task-research",
	"okt-task-validate",
	"okt-task-requirements",
	"okt-task-prioritize",
	"okt-task-create",
	"okt-task-decompose",
	"okt-task-estimate",
	"okt-task-design",
	"okt-project-resume",
	"okt-project-continue",
	"okt-plan-create",
	"okt-plan-show",
	"okt-plan-continue",
	"okt-plan-claim",
	"okt-task-resume",
	"okt-task-continue",
	"okt-task-implement",
	"okt-task-self-review",
	"okt-task-refactor",
	"okt-task-document",
	"okt-task-debrief",
	"okt-config",
	"okt-skill",
	"okt-task-commit",
	"okt-task-review",
	"okt-task-secure",
	"okt-task-check",
	"okt-task-quality",
	"okt-audit",
	"okt-pause",
	"okt-note-free",
	"okt-note-recap",
	"okt-note-list",
	"okt-note-show",
}

// commandSlugs is the membership set built once from commandTable so the
// resolver can validate a name in O(1) without rebuilding a map per call.
var commandSlugs = func() map[string]struct{} {
	out := make(map[string]struct{}, len(commandTable))
	for _, slug := range commandTable {
		out[slug] = struct{}{}
	}
	return out
}()

// isKnownCommand reports whether name (after trimming) is a registered `okt-*`
// command slug.
func isKnownCommand(name string) bool {
	_, ok := commandSlugs[strings.TrimSpace(name)]
	return ok
}

// IsRegisteredCommand reports whether name is a registered `okt-*` command slug.
// Exported for the MCP adapter's no-service path, which must distinguish a known
// command (resolve to a registered-only message) from an unknown one (error)
// without a wired catalog.
func IsRegisteredCommand(name string) bool {
	return isKnownCommand(name)
}

// CommandNames returns the canonical, ordered list of `okt-*` prompts the MCP
// adapter exposes. Order mirrors the REST-style handoff cycle so prompts/list
// answers in the order a user would naturally invoke them.
func CommandNames() []string {
	out := make([]string, 0, len(commandTable))
	out = append(out, commandTable...)
	return out
}

// playbookSlugForCommand maps a command slug to the slug of its bound
// `okt-<slug>-playbook` skill — the deterministic naming convention every
// preset's mcp_commands binding follows. The bare `okt` shortcut shares
// `okt-start`'s playbook so it renders the smart-entry concierge body, exactly
// as the bare `okt` and explicit `okt-start` share a persona/skill binding.
//
// It returns the conventional playbook slug for ANY trimmed `okt-*` input; the
// caller resolves it against the live skill catalog and degrades gracefully
// when no such skill is bound (an unwired runtime, a partial kit).
func playbookSlugForCommand(name string) string {
	name = strings.TrimSpace(name)
	if name == "okt" {
		name = "okt-start"
	}
	return name + "-playbook"
}

package agent

import "strings"

// CommandTier classifies an `okt-*` command into one of the three routing
// tiers the v2 command surface (#371) distinguishes for help + routing:
//
//   - orchestrator — bare, primary path, cross-object, director role
//     (Concierge/Owner); delegates to specialists. e.g. okt, okt-run.
//   - system — bare, talks to the tool not the project (no object).
//     e.g. okt-help, okt-config, okt-skill.
//   - granular — object-namespaced (`okt-<object>-<verb>`), power-user,
//     surgical. e.g. okt-task-implement, okt-plan-create.
//
// The tier is the discriminator help screens group on and the router uses to
// decide whether a slug carries an object (granular) or operates bare.
type CommandTier string

const (
	CommandTierOrchestrator CommandTier = "orchestrator"
	CommandTierSystem       CommandTier = "system"
	CommandTierGranular     CommandTier = "granular"
)

// CommandDescriptor is the decoded shape of a command slug. Object is non-empty
// only for the granular tier; Verb is the action segment (empty for the bare
// `okt` alias). This is the registry's structural view of a slug — it does not
// assert the command exists in any catalog, only that the slug is well-formed
// and which tier/object/verb it resolves to.
type CommandDescriptor struct {
	Slug   string      `json:"slug"`
	Tier   CommandTier `json:"tier"`
	Object string      `json:"object,omitempty"`
	Verb   string      `json:"verb,omitempty"`
}

// commandObjects is the set of object namespaces a granular slug may carry.
// A slug shaped `okt-<object>-<verb>` whose first segment is one of these
// resolves as granular with that object; anything else stays bare.
var commandObjects = map[string]struct{}{
	"task":    {},
	"plan":    {},
	"project": {},
	"note":    {},
}

// orchestratorVerbs are the bare verbs that resolve as the orchestrator tier
// (primary path, director role). The empty verb — the bare `okt` alias — is
// handled separately in DescribeCommand.
var orchestratorVerbs = map[string]struct{}{
	"start": {},
	"shape": {},
	"run":   {},
	"audit": {},
	"pause": {},
}

// systemVerbs are the bare verbs that resolve as the system tier (talk to the
// tool, no project object).
var systemVerbs = map[string]struct{}{
	"help":   {},
	"config": {},
	"skill":  {},
}

// DescribeCommand decodes an `okt-*` slug into its tier, object, and verb. It
// reports ok=false for slugs that are empty, missing the `okt` prefix, or
// malformed (trailing-hyphen, empty object/verb segment, or an unrecognized
// bare verb). The bare `okt` alias resolves as an orchestrator with an empty
// object and verb.
//
// Resolution order:
//   - bare `okt` → orchestrator
//   - `okt-<object>-<verb>` where object ∈ commandObjects → granular
//   - `okt-<verb>` where verb ∈ orchestratorVerbs → orchestrator
//   - `okt-<verb>` where verb ∈ systemVerbs → system
//   - anything else → not ok
//
// It is intentionally catalog-free: CW1 ships the routing machinery; later
// waves register the actual command set against these tiers.
func DescribeCommand(slug string) (CommandDescriptor, bool) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return CommandDescriptor{}, false
	}
	if slug == "okt" {
		return CommandDescriptor{Slug: slug, Tier: CommandTierOrchestrator}, true
	}
	rest, ok := strings.CutPrefix(slug, "okt-")
	if !ok || rest == "" {
		return CommandDescriptor{}, false
	}

	// Object-namespaced granular slug: `<object>-<verb>`.
	if obj, verb, found := strings.Cut(rest, "-"); found {
		if _, isObject := commandObjects[obj]; isObject {
			if verb == "" {
				return CommandDescriptor{}, false
			}
			return CommandDescriptor{Slug: slug, Tier: CommandTierGranular, Object: obj, Verb: verb}, true
		}
	}

	// Bare verb — classify against the orchestrator/system sets. A bare slug
	// whose verb is unknown is not a valid command (CW1 will not guess a tier
	// for it).
	if _, isOrch := orchestratorVerbs[rest]; isOrch {
		return CommandDescriptor{Slug: slug, Tier: CommandTierOrchestrator, Verb: rest}, true
	}
	if _, isSys := systemVerbs[rest]; isSys {
		return CommandDescriptor{Slug: slug, Tier: CommandTierSystem, Verb: rest}, true
	}
	return CommandDescriptor{}, false
}

package app

import (
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// BundleSwapper is the write-side of every provider in this package. A
// process holds exactly one provider set; calling Swap atomically rotates
// the underlying snapshot so concurrent readers either observe the prior
// bundle or the new one but never a half-built mix. The pointer is owned
// by the swapper after the call — callers must not mutate the bundle
// post-Swap.
//
// Phase 2 wires every config-reading service to a provider; Phase 3 lets
// the swapper carry per-project bundles. The interface stays unchanged
// because the snapshot already covers the entire bundle surface.
type BundleSwapper interface {
	Swap(bundle *config.Bundle)
}

// WorkflowProvider exposes the workflow surface that previously lived in
// SQL (workflows / workflow_buckets / workflow_transitions /
// transition_guards). Reads are O(1) — the snapshot pre-builds lookup
// maps at Swap time so hot paths (task create, task move, guard
// evaluation) never hit a slice scan.
type WorkflowProvider interface {
	// Workflow returns the active workflow with its inflated buckets and
	// transitions. The value is a deep copy of the snapshot so callers can
	// mutate freely without leaking into other readers.
	Workflow() domain.Workflow
	// BucketByID resolves a bucket by its stable yaml id. ok=false when no
	// bucket with that id exists in the active workflow.
	BucketByID(id int64) (bucket domain.Bucket, ok bool)
	// BucketByKey resolves a bucket by its key (the slug). ok=false when no
	// bucket with that key exists.
	BucketByKey(key string) (bucket domain.Bucket, ok bool)
	// Transitions returns the full list of declared transitions in
	// position order. Used by MCP/CLI surfaces that echo the workflow
	// shape to the agent.
	Transitions() []domain.WorkflowTransition
	// Guards returns the guard list declared on the (from, to) transition.
	// An empty slice means "transition exists but no guards"; nil means
	// "no such transition" — callers typically check TransitionAllowed
	// first.
	Guards(fromID, toID int64) []domain.TransitionGuard
	// IsFinalBucket reports whether the bucket sits at the highest
	// position in the active workflow — used to decide whether a move
	// should also emit task.completed.
	IsFinalBucket(id int64) bool
	// TransitionAllowed reports whether the active workflow declares a
	// transition between two bucket ids.
	TransitionAllowed(fromID, toID int64) bool
	// Operations returns the workflow-level operation policy (archive /
	// delete / unarchive guards). Returned by value so callers cannot
	// mutate the snapshot.
	Operations() domain.WorkflowOperations
}

// PersonaProvider exposes the resolved persona catalog. Personas are
// keyed by slug; the snapshot also pre-resolves the skill and law lists
// referenced from omakiten.yaml so callers do not need a second lookup.
type PersonaProvider interface {
	Personas() []config.Persona
	PersonaBySlug(slug string) (config.Persona, bool)
}

// SkillProvider exposes the resolved skill catalog. Skills carry no
// cross-references so the snapshot is a plain slice + slug index.
type SkillProvider interface {
	Skills() []config.Skill
	SkillBySlug(slug string) (config.Skill, bool)
}

// LawProvider exposes the resolved law catalog. Laws may be scoped to a
// project or persona; callers that need a scope-filtered view filter the
// returned slice — the provider keeps the snapshot simple.
type LawProvider interface {
	Laws() []config.Law
	LawBySlug(slug string) (config.Law, bool)
}

// TemplateProvider exposes the resolved template catalog. ActiveDefault
// returns the template that should be used as the scaffold for `kind`
// in the context of `projectSlug`, mirroring config.Bundle.TemplateByDefault.
type TemplateProvider interface {
	Templates() []config.TaskTemplate
	TemplateBySlug(slug string) (config.TaskTemplate, bool)
	ActiveDefault(kind, projectSlug string) (config.TaskTemplate, bool)
}

// NotificationProvider exposes the resolved notification map. The
// underlying snapshot stores the map directly; the returned copy is
// safe to mutate without affecting other readers.
type NotificationProvider interface {
	Notifications() map[string]config.Notification
	NotificationBySlug(slug string) (config.Notification, bool)
}

// MCPCommandProvider exposes the resolved mcp_commands map. The reserved
// `global` entry stays in the map; callers that want the per-command
// effective list use Resolve to apply inheritance + opt-outs.
type MCPCommandProvider interface {
	MCPCommands() map[string]config.MCPCommandSpec
	MCPCommandByKey(key string) (config.MCPCommandSpec, bool)
}

// ProviderSet bundles every per-entity provider plus the swapper. Used
// by service constructors and by the CLI/TUI/MCP boot path so the entire
// provider surface flows through a single dependency.
type ProviderSet interface {
	BundleSwapper
	WorkflowProvider
	PersonaProvider
	SkillProvider
	LawProvider
	TemplateProvider
	NotificationProvider
	MCPCommandProvider
}

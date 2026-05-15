package config

import (
	"sync/atomic"

	"omakiten/internal/domain"
)

// InMemoryProviders is the canonical implementation of every provider
// interface declared in internal/app. A single instance backs the entire
// process: each ProviderSet handle returned by NewInMemoryProviders is
// the same underlying object. Calling Swap rotates the snapshot pointer
// atomically; concurrent readers either see the prior bundle or the new
// one — never a half-built mix — because every read goes through one
// atomic.Pointer load.
//
// Phase 2 keeps the cache size at 1 (process-wide single bundle). Phase 3
// introduces per-project bundles; the snapshot pointer becomes a map
// keyed by project id without touching this interface surface.
type InMemoryProviders struct {
	snap atomic.Pointer[providerSnapshot]
}

// NewInMemoryProviders returns a provider set seeded from the given
// bundle. Pass a zero-value Bundle to construct an empty set (every
// lookup misses); production code seeds from config.LoadBundle.
func NewInMemoryProviders(bundle Bundle) *InMemoryProviders {
	p := &InMemoryProviders{}
	p.Swap(&bundle)
	return p
}

// Swap publishes a new snapshot built from the given bundle. After
// Swap returns, every subsequent provider call sees the new state; any
// reader mid-call continues with the previous snapshot until it returns.
//
// The bundle pointer is consumed by the swapper — callers must not
// mutate the bundle after the call. Pass a fresh value or a copy if the
// caller needs to keep editing.
func (p *InMemoryProviders) Swap(bundle *Bundle) {
	if bundle == nil {
		empty := Bundle{}
		bundle = &empty
	}
	p.snap.Store(buildSnapshot(*bundle))
}

// current returns the active snapshot. Always non-nil after construction.
func (p *InMemoryProviders) current() *providerSnapshot { return p.snap.Load() }

// Clone returns an independent provider handle that observes the same
// snapshot this provider currently exposes. The clone is unaffected by
// subsequent Swap calls on the original — used by the orphan-detection
// flow to keep a stable view of the pre-import bundle while the active
// providers rotate to the new one.
func (p *InMemoryProviders) Clone() *InMemoryProviders {
	clone := &InMemoryProviders{}
	clone.snap.Store(p.current())
	return clone
}

// providerSnapshot is the precomputed view of a bundle that every read
// method consults. Building the snapshot at Swap time means hot paths
// (task move, guard evaluation, template resolution) never scan slices
// or build maps — they just index.
type providerSnapshot struct {
	workflow         domain.Workflow
	bucketByID       map[int64]domain.Bucket
	bucketByKey      map[string]domain.Bucket
	finalBucketID    int64
	transitionsByPair map[transitionKey]transitionEntry
	// operations live on snap.workflow.Operations — duplicating the
	// field on the snapshot was a buildSnapshot leftover. Operations()
	// reads through workflow so the snapshot stays single-source.

	personas      []Persona
	personasBySlug map[string]Persona

	skills      []Skill
	skillsBySlug map[string]Skill

	laws      []Law
	lawsBySlug map[string]Law

	templates      []TaskTemplate
	templatesBySlug map[string]TaskTemplate
	templatesByDefault map[templateDefaultKey]TaskTemplate

	notifications map[string]Notification
	mcpCommands   map[string]MCPCommandSpec

	priorities []PriorityDefinition
	severities []SeverityDefinition

	settings Settings
}

type transitionKey struct{ from, to int64 }

type transitionEntry struct {
	guards []domain.TransitionGuard
}

type templateDefaultKey struct{ kind, project string }

// buildSnapshot inflates a Bundle into the precomputed view used by
// every provider read. The conversion mirrors the SQL inflation that
// internal/sqlite/bundles.go used to perform on import; once Phase 2
// services migrate to providers, the SQL inflation is deleted.
func buildSnapshot(bundle Bundle) *providerSnapshot {
	snap := &providerSnapshot{
		bucketByID:         map[int64]domain.Bucket{},
		bucketByKey:        map[string]domain.Bucket{},
		transitionsByPair:  map[transitionKey]transitionEntry{},
		personasBySlug:     map[string]Persona{},
		skillsBySlug:       map[string]Skill{},
		lawsBySlug:         map[string]Law{},
		templatesBySlug:    map[string]TaskTemplate{},
		templatesByDefault: map[templateDefaultKey]TaskTemplate{},
		notifications:      map[string]Notification{},
		mcpCommands:        map[string]MCPCommandSpec{},
	}

	// Workflow: pick the active workflow from settings; fall back to the
	// first declared workflow when settings is silent (matches the
	// legacy SQL resolution which used `settings.value = workflows.key`).
	if wf, ok := activeWorkflow(bundle); ok {
		snap.workflow = toDomainWorkflow(wf)
		var maxPos int
		var maxID int64
		for _, b := range snap.workflow.Buckets {
			snap.bucketByID[b.ID] = b
			snap.bucketByKey[b.Key] = b
			if b.Position > maxPos {
				maxPos = b.Position
				maxID = b.ID
			}
		}
		snap.finalBucketID = maxID
		// Guards are not surfaced on domain.WorkflowTransition (callers
		// fetch them through Guards(from,to)); snapshot them in the
		// pair-keyed map sourced from the yaml shape so we keep the
		// inflation in one place.
		for _, tr := range wf.Transitions {
			from, fromOK := snap.bucketByID[int64(tr.From)]
			to, toOK := snap.bucketByID[int64(tr.To)]
			if !fromOK || !toOK {
				continue
			}
			snap.transitionsByPair[transitionKey{from: from.ID, to: to.ID}] = transitionEntry{guards: toDomainGuards(tr.Guards, nil)}
		}
	}

	snap.personas = append(snap.personas, bundle.Personas...)
	for _, p := range bundle.Personas {
		snap.personasBySlug[p.Slug] = p
	}

	snap.skills = append(snap.skills, bundle.Skills...)
	for _, s := range bundle.Skills {
		snap.skillsBySlug[s.Slug] = s
	}

	snap.laws = append(snap.laws, bundle.Laws...)
	for _, l := range bundle.Laws {
		snap.lawsBySlug[l.Slug] = l
	}

	snap.templates = append(snap.templates, bundle.Templates...)
	for _, t := range bundle.Templates {
		snap.templatesBySlug[t.Slug] = t
		if t.Default != "" {
			snap.templatesByDefault[templateDefaultKey{kind: t.Default, project: t.ProjectSlug}] = t
		}
	}

	for k, n := range bundle.Notifications {
		snap.notifications[k] = n
	}
	for k, c := range bundle.MCPCommands {
		snap.mcpCommands[k] = c
	}

	snap.priorities = append(snap.priorities, bundle.Config.Priorities...)
	snap.severities = append(snap.severities, bundle.Config.Severities...)
	snap.settings = bundle.Config

	return snap
}

// Priorities returns the configured priority table from the active
// bundle. Used by the Store-side enum registry rebuild path.
func (p *InMemoryProviders) Priorities() []PriorityDefinition {
	return append([]PriorityDefinition(nil), p.current().priorities...)
}

// Severities returns the configured severity table from the active
// bundle. Used by sqlite delegators that resolve label → id.
func (p *InMemoryProviders) Severities() []SeverityDefinition {
	return append([]SeverityDefinition(nil), p.current().severities...)
}

// SeverityIDByLabel resolves a severity label to its configured id.
// Returns 0 when the label is empty or unknown — callers treat 0 as
// "label missing" and fall back to the bundle's default severity.
func (p *InMemoryProviders) SeverityIDByLabel(label string) int {
	if label == "" {
		return 0
	}
	for _, sev := range p.current().severities {
		if sev.Value == label {
			return sev.ID
		}
	}
	return 0
}

// Settings returns the resolved Settings block from the active bundle.
// Used by sqlite delegators (ContextSettings) and the composition root
// to read config knobs without re-parsing the YAML.
func (p *InMemoryProviders) Settings() Settings {
	return p.current().settings
}

// activeWorkflow returns the workflow chosen by `config.workflow.active`,
// falling back to the first declared workflow when the setting is unset
// or names an unknown workflow. Mirrors the SQL resolution.
func activeWorkflow(bundle Bundle) (Workflow, bool) {
	if len(bundle.Workflows) == 0 {
		return Workflow{}, false
	}
	wanted := bundle.Config.Workflow.Active
	if wanted == "" {
		return bundle.Workflows[0], true
	}
	for _, wf := range bundle.Workflows {
		if wf.Key == wanted {
			return wf, true
		}
	}
	return bundle.Workflows[0], true
}

// toDomainWorkflow converts the YAML workflow into the runtime
// representation. Bucket ids carry through as int64; transition pairs
// resolve from `from`/`to` keys (yaml) to the bucket ids built above.
func toDomainWorkflow(wf Workflow) domain.Workflow {
	out := domain.Workflow{
		ID:   int64(wf.ID),
		Key:  wf.Key,
		Name: wf.Name,
		Operations: domain.WorkflowOperations{
			Archive:   domain.OperationPolicy{Guards: toDomainGuards(wf.Operations.Archive.Guards, nil)},
			Delete:    domain.OperationPolicy{Guards: toDomainGuards(wf.Operations.Delete.Guards, nil)},
			Unarchive: domain.OperationPolicy{Guards: toDomainGuards(wf.Operations.Unarchive.Guards, nil)},
		},
	}
	if wf.Defaults != nil {
		out.Defaults = &domain.WorkflowDefaults{
			Task:    toDomainPermission(wf.Defaults.Task),
			Comment: toDomainPermission(wf.Defaults.Comment),
		}
	}

	bucketsByID := map[int]Bucket{}
	for _, b := range wf.Buckets {
		bucketsByID[b.ID] = b
		out.Buckets = append(out.Buckets, domain.Bucket{
			ID:          int64(b.ID),
			Key:         b.Key,
			Name:        b.Name,
			Position:    b.Position,
			Permissions: toDomainBucketPerms(b.Permissions),
		})
	}

	for _, tr := range wf.Transitions {
		from, fromOK := bucketsByID[tr.From]
		to, toOK := bucketsByID[tr.To]
		if !fromOK || !toOK {
			continue
		}
		out.Transitions = append(out.Transitions, domain.WorkflowTransition{
			FromBucketID:  int64(from.ID),
			FromBucketKey: from.Key,
			ToBucketID:    int64(to.ID),
			ToBucketKey:   to.Key,
		})
	}
	return out
}

// toDomainGuards converts the YAML guard list into the runtime guard
// shape. The optional `keyByName` map is unused today — guards carry
// their bucket keys directly — but kept on the signature so future
// guard types that reference bucket ids can resolve them at snapshot
// time without re-reading the bundle.
func toDomainGuards(in []TransitionGuard, _ map[string]Bucket) []domain.TransitionGuard {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.TransitionGuard, len(in))
	for i, g := range in {
		out[i] = domain.TransitionGuard{
			Type:    g.Type,
			Buckets: append([]string(nil), g.Buckets...),
			Count:   g.Count,
			Tag:     g.Tag,
			Hint:    g.Hint,
		}
	}
	return out
}

func toDomainPermission(in *EntityPermission) *domain.EntityPermission {
	if in == nil {
		return nil
	}
	return &domain.EntityPermission{Edit: copyBool(in.Edit), Delete: copyBool(in.Delete)}
}

func toDomainBucketPerms(in *BucketPermissions) *domain.BucketPermissions {
	if in == nil {
		return nil
	}
	return &domain.BucketPermissions{
		Task:    toDomainPermission(in.Task),
		Comment: toDomainPermission(in.Comment),
	}
}

func copyBool(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

// --- WorkflowProvider ---

func (p *InMemoryProviders) Workflow() domain.Workflow {
	snap := p.current()
	out := snap.workflow
	out.Buckets = append([]domain.Bucket(nil), snap.workflow.Buckets...)
	out.Transitions = append([]domain.WorkflowTransition(nil), snap.workflow.Transitions...)
	return out
}

func (p *InMemoryProviders) BucketByID(id int64) (domain.Bucket, bool) {
	b, ok := p.current().bucketByID[id]
	return b, ok
}

func (p *InMemoryProviders) BucketByKey(key string) (domain.Bucket, bool) {
	b, ok := p.current().bucketByKey[key]
	return b, ok
}

func (p *InMemoryProviders) Transitions() []domain.WorkflowTransition {
	snap := p.current()
	return append([]domain.WorkflowTransition(nil), snap.workflow.Transitions...)
}

func (p *InMemoryProviders) Guards(fromID, toID int64) []domain.TransitionGuard {
	entry, ok := p.current().transitionsByPair[transitionKey{from: fromID, to: toID}]
	if !ok {
		return nil
	}
	return append([]domain.TransitionGuard(nil), entry.guards...)
}

func (p *InMemoryProviders) IsFinalBucket(id int64) bool {
	snap := p.current()
	return snap.finalBucketID != 0 && snap.finalBucketID == id
}

func (p *InMemoryProviders) TransitionAllowed(fromID, toID int64) bool {
	_, ok := p.current().transitionsByPair[transitionKey{from: fromID, to: toID}]
	return ok
}

func (p *InMemoryProviders) Operations() domain.WorkflowOperations {
	return p.current().workflow.Operations
}

// --- PersonaProvider ---

func (p *InMemoryProviders) Personas() []Persona {
	return append([]Persona(nil), p.current().personas...)
}

func (p *InMemoryProviders) PersonaBySlug(slug string) (Persona, bool) {
	v, ok := p.current().personasBySlug[slug]
	return v, ok
}

// --- SkillProvider ---

func (p *InMemoryProviders) Skills() []Skill {
	return append([]Skill(nil), p.current().skills...)
}

func (p *InMemoryProviders) SkillBySlug(slug string) (Skill, bool) {
	v, ok := p.current().skillsBySlug[slug]
	return v, ok
}

// --- LawProvider ---

func (p *InMemoryProviders) Laws() []Law {
	return append([]Law(nil), p.current().laws...)
}

func (p *InMemoryProviders) LawBySlug(slug string) (Law, bool) {
	v, ok := p.current().lawsBySlug[slug]
	return v, ok
}

// --- TemplateProvider ---

func (p *InMemoryProviders) Templates() []TaskTemplate {
	return append([]TaskTemplate(nil), p.current().templates...)
}

func (p *InMemoryProviders) TemplateBySlug(slug string) (TaskTemplate, bool) {
	v, ok := p.current().templatesBySlug[slug]
	return v, ok
}

// ActiveDefault mirrors Bundle.TemplateByDefault: project-scoped wins,
// global default falls back. Returns ok=false when neither layer
// declares an active scaffold for `kind`.
func (p *InMemoryProviders) ActiveDefault(kind, projectSlug string) (TaskTemplate, bool) {
	if kind == "" {
		return TaskTemplate{}, false
	}
	snap := p.current()
	if projectSlug != "" {
		if t, ok := snap.templatesByDefault[templateDefaultKey{kind: kind, project: projectSlug}]; ok {
			return t, true
		}
	}
	if t, ok := snap.templatesByDefault[templateDefaultKey{kind: kind, project: ""}]; ok {
		return t, true
	}
	return TaskTemplate{}, false
}

// --- NotificationProvider ---

func (p *InMemoryProviders) Notifications() map[string]Notification {
	src := p.current().notifications
	out := make(map[string]Notification, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (p *InMemoryProviders) NotificationBySlug(slug string) (Notification, bool) {
	v, ok := p.current().notifications[slug]
	return v, ok
}

// --- MCPCommandProvider ---

func (p *InMemoryProviders) MCPCommands() map[string]MCPCommandSpec {
	src := p.current().mcpCommands
	out := make(map[string]MCPCommandSpec, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (p *InMemoryProviders) MCPCommandByKey(key string) (MCPCommandSpec, bool) {
	v, ok := p.current().mcpCommands[key]
	return v, ok
}

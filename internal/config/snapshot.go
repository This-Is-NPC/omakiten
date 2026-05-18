package config

import (
	"omakiten/internal/domain"
)

// transitionKey indexes the snapshot's pair-keyed transition map. Pairs
// are bucket ids (int64). Workflow shape on the snapshot uses ids so
// renames are id-stable, which means transition lookup stays correct
// across a `bucket.key` rename across imports.
type transitionKey struct{ from, to int64 }

// transitionEntry carries the per-transition state the snapshot pre-builds
// at BuildSnapshot time. Today it only records the guard list; future
// per-transition policy (allow_archived?, comment_required?) lives here
// so the call paths consume one lookup per check.
type transitionEntry struct {
	guards []domain.TransitionGuard
}

// templateDefaultKey indexes the snapshot's "active default scaffold
// per kind, per project slug" map. project="" means the global default
// (used when no project-scoped template matches).
type templateDefaultKey struct{ kind, project string }

// activeWorkflow picks the workflow named in `config.workflow.active`,
// falling back to the first declared workflow when the setting is empty
// or names an unknown key. The fallback keeps single-workflow bundles
// usable without forcing the author to set `workflow.active` explicitly.
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

// Snapshot is the immutable, value-typed view of a Bundle that every app
// service consumes at construction time. Building the snapshot inflates
// the YAML shape into the runtime indexes (bucket-by-id, bucket-by-key,
// transitions, template-defaults, persona/skill/law slug maps) once;
// hot-path lookups (task move, guard evaluation, template resolve) hit
// O(1) map reads and never scan slices.
//
// Concurrency contract: a Snapshot is read-only after BuildSnapshot
// returns. There is no Swap, no mutation method, no atomic pointer
// embedded inside. Mutation flows through BundleCache, which produces a
// fresh *Snapshot for each rebuild and rotates the pointer atomically at
// the ProjectRuntime entry. In-flight callers that captured the previous
// pointer keep reading from it until they return; the new pointer is
// only visible to subsequent Resolve calls. This is the architectural
// invariant the per-project isolation relies on — N agents on N projects
// each hold their own *Snapshot, and no Store-side shared singleton
// reroutes their reads through the latest bundle.
//
// Fields are unexported; every read goes through a method that returns a
// defensive copy (slice, map) or a value (bucket, persona). That keeps
// the contract "snapshot is immutable" enforceable even when a caller
// mistakenly mutates the returned slice.
type Snapshot struct {
	workflow         domain.Workflow
	bucketByID       map[int64]domain.Bucket
	bucketByKey      map[string]domain.Bucket
	finalBucketID    int64
	transitionsByPair map[transitionKey]transitionEntry

	personas       []Persona
	personasBySlug map[string]Persona

	skills       []Skill
	skillsBySlug map[string]Skill

	laws       []Law
	lawsBySlug map[string]Law

	templates          []TaskTemplate
	templatesBySlug    map[string]TaskTemplate
	templatesByDefault map[templateDefaultKey]TaskTemplate

	notifications map[string]Notification
	mcpCommands   map[string]MCPCommandSpec

	priorities []PriorityDefinition
	severities []SeverityDefinition

	languages       []Language
	languagesByCode map[string]Language
	catalogCLI      *Catalog
	catalogTUI      *Catalog
	agentOutputLang string

	registry *domain.EnumRegistry

	settings Settings
}

// BuildSnapshot inflates a Bundle into the immutable Snapshot. Construct
// once per bundle load; pass the returned pointer to every app service
// that needs to read config. The Bundle argument is consumed by value —
// the caller is free to mutate or discard it after the call.
func BuildSnapshot(bundle Bundle) *Snapshot {
	snap := &Snapshot{
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
		languagesByCode:    map[string]Language{},
	}

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
	snap.registry = buildEnumRegistry(bundle)

	snap.languages = append(snap.languages, bundle.Languages...)
	for _, lang := range bundle.Languages {
		snap.languagesByCode[lang.Code] = lang
	}
	eff := bundle.Config.EffectiveLanguages()
	baseline := snap.lookupLanguage("en")
	snap.catalogCLI = buildSurfaceCatalog(snap.languagesByCode, eff.CLI, baseline)
	snap.catalogTUI = buildSurfaceCatalog(snap.languagesByCode, eff.TUI, baseline)
	snap.agentOutputLang = eff.AgentOutput

	return snap
}

// lookupLanguage returns the Language with code or nil. Used by the
// catalog builder to grab the baseline ("en") pointer once so both
// surface catalogs share the same fallback.
func (s *Snapshot) lookupLanguage(code string) *Language {
	if lang, ok := s.languagesByCode[code]; ok {
		return &lang
	}
	return nil
}

// buildSurfaceCatalog resolves the configured language for one surface
// into a Catalog. Unknown configured codes still produce a usable
// Catalog: active stays nil so every Get falls through to the baseline.
// Baseline may also be nil when no en pack is shipped, in which case
// Get falls through to the key literal.
func buildSurfaceCatalog(byCode map[string]Language, code string, baseline *Language) *Catalog {
	var active *Language
	if lang, ok := byCode[code]; ok {
		active = &lang
	}
	return NewCatalog(active, baseline)
}

// buildEnumRegistry inflates the bundle's priority/severity tables into the
// per-project EnumRegistry. Built once at BuildSnapshot time so every app
// service that resolves priority or severity labels reads through the same
// immutable instance — no separate registry plumbing escapes the snapshot.
func buildEnumRegistry(bundle Bundle) *domain.EnumRegistry {
	priorityPairs := make([]domain.PriorityPair, len(bundle.Config.Priorities))
	for i, p := range bundle.Config.Priorities {
		priorityPairs[i] = domain.PriorityPair{ID: p.ID, Value: p.Value, Default: p.Default}
	}
	severityPairs := make([]domain.SeverityPair, len(bundle.Config.Severities))
	for i, s := range bundle.Config.Severities {
		severityPairs[i] = domain.SeverityPair{ID: s.ID, Value: s.Value, Default: s.Default}
	}
	return domain.NewEnumRegistry(priorityPairs, severityPairs)
}

// Workflow returns the active workflow with its inflated buckets and
// transitions. The returned value carries fresh slice headers (Buckets,
// Transitions) so callers can mutate the locally-held copy without
// leaking into other readers; the slice elements themselves are values
// (domain.Bucket, domain.WorkflowTransition).
func (s *Snapshot) Workflow() domain.Workflow {
	out := s.workflow
	out.Buckets = append([]domain.Bucket(nil), s.workflow.Buckets...)
	out.Transitions = append([]domain.WorkflowTransition(nil), s.workflow.Transitions...)
	return out
}

// BucketByID resolves a bucket by its stable yaml id. ok=false when no
// bucket with that id exists in the active workflow.
func (s *Snapshot) BucketByID(id int64) (domain.Bucket, bool) {
	b, ok := s.bucketByID[id]
	return b, ok
}

// BucketByKey resolves a bucket by its key (the slug). ok=false when no
// bucket with that key exists.
func (s *Snapshot) BucketByKey(key string) (domain.Bucket, bool) {
	b, ok := s.bucketByKey[key]
	return b, ok
}

// Transitions returns the full list of declared transitions in
// position order. The slice is a fresh copy.
func (s *Snapshot) Transitions() []domain.WorkflowTransition {
	return append([]domain.WorkflowTransition(nil), s.workflow.Transitions...)
}

// Guards returns the guard list declared on the (from, to) transition.
// An empty slice means "transition exists but no guards"; nil means "no
// such transition" — callers typically check TransitionAllowed first.
func (s *Snapshot) Guards(fromID, toID int64) []domain.TransitionGuard {
	entry, ok := s.transitionsByPair[transitionKey{from: fromID, to: toID}]
	if !ok {
		return nil
	}
	return append([]domain.TransitionGuard(nil), entry.guards...)
}

// IsFinalBucket reports whether the bucket sits at the highest position
// in the active workflow — used to decide whether a move should also
// emit task.completed.
func (s *Snapshot) IsFinalBucket(id int64) bool {
	return s.finalBucketID != 0 && s.finalBucketID == id
}

// TransitionAllowed reports whether the active workflow declares a
// transition between two bucket ids.
func (s *Snapshot) TransitionAllowed(fromID, toID int64) bool {
	_, ok := s.transitionsByPair[transitionKey{from: fromID, to: toID}]
	return ok
}

// Operations returns the workflow-level operation policy (archive /
// delete / unarchive guards). Returned by value so callers cannot mutate
// the snapshot.
func (s *Snapshot) Operations() domain.WorkflowOperations {
	return s.workflow.Operations
}

// Personas returns the resolved persona catalog. The slice is a fresh
// copy.
func (s *Snapshot) Personas() []Persona {
	return append([]Persona(nil), s.personas...)
}

// PersonaBySlug resolves a persona by slug. ok=false when no persona
// with that slug exists.
func (s *Snapshot) PersonaBySlug(slug string) (Persona, bool) {
	v, ok := s.personasBySlug[slug]
	return v, ok
}

// Skills returns the resolved skill catalog. The slice is a fresh copy.
func (s *Snapshot) Skills() []Skill {
	return append([]Skill(nil), s.skills...)
}

// SkillBySlug resolves a skill by slug.
func (s *Snapshot) SkillBySlug(slug string) (Skill, bool) {
	v, ok := s.skillsBySlug[slug]
	return v, ok
}

// Laws returns the resolved law catalog. The slice is a fresh copy.
func (s *Snapshot) Laws() []Law {
	return append([]Law(nil), s.laws...)
}

// LawBySlug resolves a law by slug.
func (s *Snapshot) LawBySlug(slug string) (Law, bool) {
	v, ok := s.lawsBySlug[slug]
	return v, ok
}

// Templates returns the resolved template catalog. The slice is a
// fresh copy.
func (s *Snapshot) Templates() []TaskTemplate {
	return append([]TaskTemplate(nil), s.templates...)
}

// Languages returns every Language discovered by the loader (bundled +
// custom, dedup by code with custom winning). The slice is a fresh
// copy in deterministic code-ascending order so callers may sort,
// filter, or mutate without disturbing the snapshot.
func (s *Snapshot) Languages() []Language {
	return append([]Language(nil), s.languages...)
}

// LanguageByCode resolves a Language by its lowercase code. ok=false
// when no Language with that code is loaded — callers may then fall
// back to the en baseline or surface a "not found" error.
func (s *Snapshot) LanguageByCode(code string) (Language, bool) {
	v, ok := s.languagesByCode[code]
	return v, ok
}

// Catalog returns the resolved Catalog for the given Surface. The
// returned pointer is stable for the lifetime of the snapshot and is
// safe for concurrent reads. Callers must not retain it across a
// BundleCache.Reload — fetch a fresh Catalog from the new Snapshot.
func (s *Snapshot) Catalog(surface Surface) *Catalog {
	switch surface {
	case SurfaceTUI:
		return s.catalogTUI
	default:
		return s.catalogCLI
	}
}

// AgentOutputLanguage returns the raw configured agent-output language
// string. Empty when the user has not selected one — the MCP composer
// then skips the trailing "**Output language:** ..." directive entirely.
// Free-form by design: any non-empty string is honored verbatim because
// the agent interprets the directive against its own language training,
// not the discovered catalog.
func (s *Snapshot) AgentOutputLanguage() string {
	return s.agentOutputLang
}

// ResolveGuardHint expands `${{intl:KEY}}` tokens inside a guard hint
// string against the CLI catalog. Exposed as a string-in / string-out
// projection so the guards evaluator (internal/app/guards) can resolve
// hints without naming Catalog/Surface — see
// internal/arch/i18n_boundary_test.go. Empty input returns empty.
func (s *Snapshot) ResolveGuardHint(hint string) string {
	if hint == "" {
		return ""
	}
	return s.Catalog(SurfaceCLI).Resolve(hint)
}

// TemplateBySlug resolves a template by slug.
func (s *Snapshot) TemplateBySlug(slug string) (TaskTemplate, bool) {
	v, ok := s.templatesBySlug[slug]
	return v, ok
}

// ActiveDefault mirrors Bundle.TemplateByDefault: project-scoped wins,
// global default falls back. Returns ok=false when neither layer
// declares an active scaffold for `kind`.
func (s *Snapshot) ActiveDefault(kind, projectSlug string) (TaskTemplate, bool) {
	if kind == "" {
		return TaskTemplate{}, false
	}
	if projectSlug != "" {
		if t, ok := s.templatesByDefault[templateDefaultKey{kind: kind, project: projectSlug}]; ok {
			return t, true
		}
	}
	if t, ok := s.templatesByDefault[templateDefaultKey{kind: kind, project: ""}]; ok {
		return t, true
	}
	return TaskTemplate{}, false
}

// Notifications returns a fresh copy of the resolved notification map.
func (s *Snapshot) Notifications() map[string]Notification {
	out := make(map[string]Notification, len(s.notifications))
	for k, v := range s.notifications {
		out[k] = v
	}
	return out
}

// NotificationBySlug resolves a notification by slug.
func (s *Snapshot) NotificationBySlug(slug string) (Notification, bool) {
	v, ok := s.notifications[slug]
	return v, ok
}

// MCPCommands returns a fresh copy of the resolved mcp_commands map.
// The reserved `global` entry stays in the map; callers that want the
// per-command effective list use Resolve to apply inheritance + opt-outs.
func (s *Snapshot) MCPCommands() map[string]MCPCommandSpec {
	out := make(map[string]MCPCommandSpec, len(s.mcpCommands))
	for k, v := range s.mcpCommands {
		out[k] = v
	}
	return out
}

// MCPCommandByKey resolves an mcp_command entry by key.
func (s *Snapshot) MCPCommandByKey(key string) (MCPCommandSpec, bool) {
	v, ok := s.mcpCommands[key]
	return v, ok
}

// Priorities returns the configured priority table. Used by the enum
// registry rebuild path.
func (s *Snapshot) Priorities() []PriorityDefinition {
	return append([]PriorityDefinition(nil), s.priorities...)
}

// Severities returns the configured severity table.
func (s *Snapshot) Severities() []SeverityDefinition {
	return append([]SeverityDefinition(nil), s.severities...)
}

// SeverityIDByLabel resolves a severity label to its configured id.
// Returns 0 when the label is empty or unknown — callers treat 0 as
// "label missing" and fall back to the bundle's default severity.
func (s *Snapshot) SeverityIDByLabel(label string) int {
	if label == "" {
		return 0
	}
	for _, sev := range s.severities {
		if sev.Value == label {
			return sev.ID
		}
	}
	return 0
}

// Settings returns the resolved Settings block. The value carries the
// full configuration tree (mcp, context, sqlite, activity_log, events,
// search, hooks, …) so callers reading any process-scope or per-call
// knob go through this single accessor.
func (s *Snapshot) Settings() Settings {
	return s.settings
}

// Synonyms returns the configured tag-synonym map (alias → canonical).
// Snapshot is immutable, so callers receive the underlying map by
// reference — must not mutate. Nil when the receiver is nil or the
// bundle declares no synonyms; callers can call without a nil check.
// The shared-reference path is the tag-normalize hot path
// (NormalizeTagName fires per tag per comment / error / task), so the
// previous defensive copy was a per-call allocation no caller needed.
func (s *Snapshot) Synonyms() map[string]string {
	if s == nil || len(s.settings.TagSynonyms) == 0 {
		return nil
	}
	return s.settings.TagSynonyms
}

// Stopwords returns the configured search-stopword list. Snapshot is
// immutable, so callers receive the underlying slice by reference —
// must not mutate. Nil when the receiver is nil.
func (s *Snapshot) Stopwords() []string {
	if s == nil {
		return nil
	}
	return s.settings.Search.Stopwords
}

// Hooks returns a fresh copy of the configured hook list. The
// composition root passes this to hooks.NewEngine so each project gets
// a per-runtime engine seeded with its own bundle's hooks.
func (s *Snapshot) Hooks() []HookSpec {
	return append([]HookSpec(nil), s.settings.Hooks...)
}

// Events returns the resolved events policy block. Composition root
// reads this when wiring per-project event-bus channels and per-event
// log/broadcast/hook gating.
func (s *Snapshot) Events() EventsSettings {
	return s.settings.Events
}

// Registry returns the per-project EnumRegistry built at BuildSnapshot time
// from the bundle's priority and severity tables. App services that resolve
// priority or severity labels read through this single instance — there is
// no separate registry pointer threaded alongside the snapshot.
func (s *Snapshot) Registry() *domain.EnumRegistry {
	return s.registry
}

// ContextSettings returns the resolved context-window block. Used by
// app.ContextService to clamp recent-context shipments.
func (s *Snapshot) ContextSettings() domain.ContextSettings {
	return domain.ContextSettings{
		DefaultLevel: s.settings.Context.DefaultLevel,
		MaxTokens:    s.settings.Context.MaxTokens,
	}
}

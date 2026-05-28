package config

import (
	"strings"
	"time"

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
	kit                Kit
	subtaskKitPath     string
	subtaskKitSnapshot *Snapshot

	workflow          domain.Workflow
	bucketByID        map[int64]domain.Bucket
	bucketByKey       map[string]domain.Bucket
	finalBucketID     int64
	transitionsByPair map[transitionKey]transitionEntry

	// Per-entity slices are the storage of record. The *BySlug maps
	// hold the slice index (not a value copy) so the snapshot stores
	// each Persona / Skill / Law / TaskTemplate exactly once.
	personas       []Persona
	personasBySlug map[string]int

	skills       []Skill
	skillsBySlug map[string]int

	laws       []Law
	lawsBySlug map[string]int

	templates          []TaskTemplate
	templatesBySlug    map[string]int
	templatesByDefault map[templateDefaultKey]int

	notifications map[string]Notification
	mcpCommands   map[string]MCPCommandSpec

	priorities []PriorityDefinition
	severities []SeverityDefinition

	languages       []Language
	languagesByCode map[string]int
	catalogCLI      *Catalog
	catalogTUI      *Catalog
	agentOutputLang string

	registry *domain.EnumRegistry

	settings Settings
	// settingsSources mirrors Bundle.Sources: per-leaf-path origin
	// labels (SourceDefault / SourceProject / SourceEnv) used by the
	// settings viewer column. Stored by value so the snapshot stays
	// immutable; SourceFor returns SourceDefault for missing paths so
	// every EffectiveTuple gets a non-empty label.
	settingsSources map[string]string
	theme           Theme
	themeErr        error

	warnings []SourceWarning
}

// BuildSnapshot inflates a Bundle into the immutable Snapshot. Construct
// once per bundle load; pass the returned pointer to every app service
// that needs to read config. The Bundle argument is consumed by value —
// the caller is free to mutate or discard it after the call.
func BuildSnapshot(bundle Bundle) *Snapshot {
	snap := &Snapshot{
		kit:                bundle.Kit,
		subtaskKitPath:     bundle.SubtaskKit,
		bucketByID:         map[int64]domain.Bucket{},
		bucketByKey:        map[string]domain.Bucket{},
		transitionsByPair:  map[transitionKey]transitionEntry{},
		personasBySlug:     map[string]int{},
		skillsBySlug:       map[string]int{},
		lawsBySlug:         map[string]int{},
		templatesBySlug:    map[string]int{},
		templatesByDefault: map[templateDefaultKey]int{},
		notifications:      map[string]Notification{},
		mcpCommands:        map[string]MCPCommandSpec{},
		languagesByCode:    map[string]int{},
	}
	if bundle.SubtaskBundle != nil {
		snap.subtaskKitSnapshot = BuildSnapshot(*bundle.SubtaskBundle)
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
	for i, p := range snap.personas {
		snap.personasBySlug[p.Slug] = i
	}

	snap.skills = append(snap.skills, bundle.Skills...)
	for i, s := range snap.skills {
		snap.skillsBySlug[s.Slug] = i
	}

	snap.laws = append(snap.laws, bundle.Laws...)
	for i, l := range snap.laws {
		snap.lawsBySlug[l.Slug] = i
	}

	snap.templates = append(snap.templates, bundle.Templates...)
	for i, t := range snap.templates {
		snap.templatesBySlug[t.Slug] = i
		if t.Default != "" {
			snap.templatesByDefault[templateDefaultKey{kind: t.Default, project: t.ProjectSlug}] = i
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
	if len(bundle.Sources) > 0 {
		snap.settingsSources = make(map[string]string, len(bundle.Sources))
		for k, v := range bundle.Sources {
			snap.settingsSources[k] = v
		}
	}
	snap.registry = buildEnumRegistry(bundle)
	snap.theme = cloneTheme(bundle.ActiveTheme)
	snap.themeErr = bundle.ActiveThemeErr

	snap.languages = append(snap.languages, bundle.Languages...)
	for i, lang := range snap.languages {
		snap.languagesByCode[lang.Code] = i
	}
	snap.warnings = append(snap.warnings, bundle.Warnings...)

	eff := bundle.Config.EffectiveLanguages()
	baseline := snap.lookupLanguage("en")
	snap.catalogCLI = buildSurfaceCatalog(snap, eff.CLI, baseline)
	snap.catalogTUI = buildSurfaceCatalog(snap, eff.TUI, baseline)
	snap.agentOutputLang = eff.AgentOutput

	// RAM trim: every locale pack ships ~150 keys (long Hindi /
	// Marathi packs ~1000+) which translates to ~7-50 KB of map
	// overhead per pack. The picker only needs Code/Name/Native;
	// the catalogs already captured their pointers into the
	// active/baseline languages above. Drop Keys on every pack that
	// is neither the active CLI / TUI / agent-output code nor the
	// en baseline so the snapshot's residual footprint stays
	// proportional to the surfaces in use, not the total locales
	// shipped. The catalogs' pointers were taken on local copies
	// (heap-escaped via the return) so the map drop here does not
	// race-trim the catalogs.
	keep := map[string]bool{}
	if eff.CLI != "" {
		keep[eff.CLI] = true
	}
	if eff.TUI != "" {
		keep[eff.TUI] = true
	}
	if eff.AgentOutput != "" {
		keep[eff.AgentOutput] = true
	}
	keep["en"] = true
	// languagesByCode now stores slice indexes (see the snapshot dedup
	// refactor); trim Keys on the slice and the by-code lookup follows
	// because the catalogs already captured local copies of the active
	// + baseline languages before the trim ran.
	for i, lang := range snap.languages {
		if keep[lang.Code] {
			continue
		}
		snap.languages[i].Keys = nil
	}

	return snap
}

// Kit returns the identity block for the kit that produced this snapshot.
func (s *Snapshot) Kit() Kit {
	if s == nil {
		return Kit{}
	}
	return s.kit
}

// KitKey satisfies domain.BucketResolver — it returns the same string as
// `s.Kit().Key` but lets non-config callers (sqlite, hooks) read the kit
// identity through the interface without depending on the config package.
func (s *Snapshot) KitKey() string {
	if s == nil {
		return ""
	}
	return s.kit.Key
}

// SubtaskKitPath returns the root kit's raw subtask_kit path, if configured.
func (s *Snapshot) SubtaskKitPath() string {
	if s == nil {
		return ""
	}
	return s.subtaskKitPath
}

// SubtaskKit returns the loaded sub-kit snapshot, if the root kit configured
// one. Callers that need protocol settings should stay on the root snapshot;
// this child snapshot is for task-shape concerns only.
func (s *Snapshot) SubtaskKit() (*Snapshot, bool) {
	if s == nil || s.subtaskKitSnapshot == nil {
		return nil, false
	}
	return s.subtaskKitSnapshot, true
}

// For returns the task-shape snapshot that applies to the supplied task.
// Root tasks resolve to the root kit. A sub-task resolves to the configured
// sub-task kit when present; projects without subtask_kit fall back to the
// root snapshot so pre-cascade behavior is kept. Callers without a task to
// resolve should use the snapshot directly (the API previously accepted a
// variadic and silently ignored extra args — that footgun is gone).
func (s *Snapshot) For(task domain.Task) *Snapshot {
	if s == nil {
		return nil
	}
	if !task.IsSubTask() || s.subtaskKitSnapshot == nil {
		return s
	}
	return s.subtaskKitSnapshot
}

// lookupLanguage returns the Language with code or nil. Used by the
// catalog builder to grab the baseline ("en") pointer once so both
// surface catalogs share the same fallback.
func (s *Snapshot) lookupLanguage(code string) *Language {
	if idx, ok := s.languagesByCode[code]; ok {
		lang := s.languages[idx]
		return &lang
	}
	return nil
}

// buildSurfaceCatalog resolves the configured language for one surface
// into a Catalog. Unknown configured codes still produce a usable
// Catalog: active stays nil so every Get falls through to the baseline.
// Baseline may also be nil when no en pack is shipped, in which case
// Get falls through to the key literal.
func buildSurfaceCatalog(snap *Snapshot, code string, baseline *Language) *Catalog {
	return NewCatalog(snap.lookupLanguage(code), baseline)
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
	idx, ok := s.personasBySlug[slug]
	if !ok {
		return Persona{}, false
	}
	return s.personas[idx], true
}

// Skills returns the resolved skill catalog. The slice is a fresh copy.
func (s *Snapshot) Skills() []Skill {
	return append([]Skill(nil), s.skills...)
}

// SkillBySlug resolves a skill by slug.
func (s *Snapshot) SkillBySlug(slug string) (Skill, bool) {
	idx, ok := s.skillsBySlug[slug]
	if !ok {
		return Skill{}, false
	}
	return s.skills[idx], true
}

// Laws returns the resolved law catalog. The slice is a fresh copy.
func (s *Snapshot) Laws() []Law {
	return append([]Law(nil), s.laws...)
}

// LawBySlug resolves a law by slug.
func (s *Snapshot) LawBySlug(slug string) (Law, bool) {
	idx, ok := s.lawsBySlug[slug]
	if !ok {
		return Law{}, false
	}
	return s.laws[idx], true
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
	idx, ok := s.languagesByCode[code]
	if !ok {
		return Language{}, false
	}
	return s.languages[idx], true
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
	idx, ok := s.templatesBySlug[slug]
	if !ok {
		return TaskTemplate{}, false
	}
	return s.templates[idx], true
}

// ActiveDefault mirrors Bundle.TemplateByDefault: project-scoped wins,
// global default falls back. Returns ok=false when neither layer
// declares an active scaffold for `kind`.
func (s *Snapshot) ActiveDefault(kind, projectSlug string) (TaskTemplate, bool) {
	if kind == "" {
		return TaskTemplate{}, false
	}
	if projectSlug != "" {
		if idx, ok := s.templatesByDefault[templateDefaultKey{kind: kind, project: projectSlug}]; ok {
			return s.templates[idx], true
		}
	}
	if idx, ok := s.templatesByDefault[templateDefaultKey{kind: kind, project: ""}]; ok {
		return s.templates[idx], true
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

// SourceFor returns the layer label recorded for dot-path `path`
// (SourceDefault / SourceProject / SourceEnv). Empty or missing paths
// fall back to SourceDefault — the conservative answer when the
// snapshot was built from a Bundle that bypassed LoadBundle (test
// fixtures, MCP composer mocks). Used by EffectiveTuples to populate
// the per-row Source field; the TUI settings viewer (#258) consumes
// that field directly.
func (s *Snapshot) SourceFor(path string) string {
	if s == nil || s.settingsSources == nil {
		return SourceDefault
	}
	if v, ok := s.settingsSources[strings.TrimSpace(path)]; ok && v != "" {
		return v
	}
	return SourceDefault
}

// Theme returns the active theme tokens resolved by LoadBundle. Returned
// by value with a fresh Colors map so callers (style builders, markdown
// renderers) can read without disturbing other readers. When the loader
// could not resolve the active slug Theme returns a zero-Theme; callers
// that need a live theme check ThemeError() and surface ErrConfigInvalid.
//
// Replaces the per-callsite themes/<slug>.yaml disk read used before the
// Phase 2-bis Theme-via-Snapshot closure. Hot-reload now consumes the
// rotated *Snapshot pointer instead of reopening YAML inside the TUI.
func (s *Snapshot) Theme() Theme {
	return cloneTheme(s.theme)
}

// ThemeError returns the failure captured by LoadBundle while resolving
// the active theme, or nil when the theme loaded cleanly or no theme was
// requested. The error wraps the underlying loader error via fmt.Errorf
// %w so callers may errors.Is(..., os.ErrNotExist) and the rendered
// message names both candidate paths the loader considered.
func (s *Snapshot) ThemeError() error {
	return s.themeErr
}

// cloneTheme returns a deep copy of t so callers cannot mutate the
// snapshot's internal Colors map by writing into the returned value.
func cloneTheme(t Theme) Theme {
	if len(t.Colors) == 0 {
		t.Colors = nil
		return t
	}
	colors := make(map[string]string, len(t.Colors))
	for k, v := range t.Colors {
		colors[k] = v
	}
	t.Colors = colors
	return t
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

// Tricks returns the resolved trick palette settings block. The TUI
// reads this at NewModel + reloadBundle to seed the palette
// ScreenRegistry with the user's `tricks.nav` overrides; reading
// through the snapshot keeps the TUI free of the bundle editor's
// LoadBundle path (the refresh_noscan_test guard panics if the TUI
// touches the editor outside the explicit hot-reload code path).
func (s *Snapshot) Tricks() TricksSettings {
	return s.settings.Tricks
}

// Registry returns the per-project EnumRegistry built at BuildSnapshot time
// from the bundle's priority and severity tables. App services that resolve
// priority or severity labels read through this single instance — there is
// no separate registry pointer threaded alongside the snapshot.
func (s *Snapshot) Registry() *domain.EnumRegistry {
	return s.registry
}

// Warnings returns the loader-emitted soft-validation diagnostics from
// the bundle this snapshot was built from. TUI/CLI surfaces use these
// to render the same per-entity warning chip the bundle's
// editor.Load() path would; reading them off the snapshot avoids a
// disk round-trip on every TUI refresh.
func (s *Snapshot) Warnings() []SourceWarning {
	if len(s.warnings) == 0 {
		return nil
	}
	out := make([]SourceWarning, len(s.warnings))
	copy(out, s.warnings)
	return out
}

// ContextSettings returns the resolved context-window block. Used by
// app.ContextService to clamp recent-context shipments.
func (s *Snapshot) ContextSettings() domain.ContextSettings {
	return domain.ContextSettings{
		DefaultLevel: s.settings.Context.DefaultLevel,
		MaxTokens:    s.settings.Context.MaxTokens,
	}
}

// LogsWindowDays returns the configured LOGS-view default time horizon
// as a time.Duration (days × 24h). Call sites that need a timestamp
// floor can do `time.Now().Add(-snap.LogsWindowDays())` without
// re-doing the day-to-duration math themselves. The validator
// guarantees `config.views.logs.window_days > 0` at runtime, so the
// returned value is always positive.
func (s *Snapshot) LogsWindowDays() time.Duration {
	return time.Duration(s.settings.Views.Logs.WindowDays) * 24 * time.Hour
}

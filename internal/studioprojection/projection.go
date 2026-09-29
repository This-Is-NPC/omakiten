package studioprojection

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/domain"
)

// Text resolves a localized message. A nil resolver uses the supplied fallback.
type Text func(key, fallback string, args ...any) string

// Input is the complete in-memory state needed to build a Studio projection.
// Build does not retain any input slices or maps, so callers may safely replace
// a candidate after an editor interaction and build a new projection.
type Input struct {
	Bundle       config.Bundle
	Workflow     domain.Workflow
	Snapshot     *config.Snapshot
	Tasks        []domain.Task
	CommandNames []string
	HookHistory  map[int][]HookExecuted
	Text         Text
}

// Projection is the semantic read model for all four Studio surfaces.
type Projection struct {
	Bundle          config.Bundle
	Workflow        config.Workflow
	WorkflowRows    []WorkflowRow
	FlowWarnings    []string
	TaskCounts      map[string]int
	CommandRows     []CommandRow
	CommandWarnings []string
	Personas        []PersonaRow
	LawSeverities   map[string]string
	TemplateBodies  map[string]string
	Hooks           []config.HookSpec
	HookHistory     map[int][]HookExecuted
}

// Build computes every reusable Studio fact once, outside screen body
// composition.
func Build(input Input) Projection {
	bundle := input.Bundle
	workflow := bundleWorkflow(bundle, input.Workflow, input.Snapshot)
	text := input.Text
	commands := commandRows(bundle.Commands, input.CommandNames)
	personas := personaRows(bundle, input.CommandNames)
	history := cloneHistory(input.HookHistory)
	return Projection{
		Bundle:          bundle,
		Workflow:        workflow,
		WorkflowRows:    WorkflowRows(workflow, TaskCounts(input.Tasks), text),
		FlowWarnings:    FlowWarnings(workflow, text),
		TaskCounts:      TaskCounts(input.Tasks),
		CommandRows:     commands,
		CommandWarnings: CommandWarnings(bundle, text),
		Personas:        personas,
		LawSeverities:   lawSeverities(bundle),
		TemplateBodies:  templateBodies(bundle),
		Hooks:           append([]config.HookSpec(nil), bundle.Config.Hooks...),
		HookHistory:     history,
	}
}

func lawSeverities(bundle config.Bundle) map[string]string {
	laws := bundledraft.LoadedLaws(bundle)
	if len(laws) == 0 {
		return nil
	}
	out := make(map[string]string, len(laws))
	for _, law := range laws {
		out[law.Slug] = law.Severity
	}
	return out
}

func templateBodies(bundle config.Bundle) map[string]string {
	templates := bundledraft.LoadedTemplates(bundle)
	if len(templates) == 0 {
		return nil
	}
	out := make(map[string]string, len(templates))
	for _, template := range templates {
		out[template.Slug] = template.Body
	}
	return out
}

func bundleWorkflow(bundle config.Bundle, workflow domain.Workflow, snap *config.Snapshot) config.Workflow {
	if active, ok := bundledraft.ActiveWorkflow(bundle); ok {
		return active
	}
	return ConfigWorkflowFromDomain(workflow, snap)
}

// ConfigWorkflowFromDomain converts the runtime workflow read model into the
// config shape used by the staged candidate editor.
func ConfigWorkflowFromDomain(workflow domain.Workflow, snap *config.Snapshot) config.Workflow {
	out := config.Workflow{ID: int(workflow.ID), Key: workflow.Key, Name: workflow.Name}
	for _, bucket := range workflow.Buckets {
		out.Buckets = append(out.Buckets, config.Bucket{ID: int(bucket.ID), Key: bucket.Key, Name: bucket.Name, Position: bucket.Position})
	}
	for _, transition := range workflow.Transitions {
		item := config.Transition{From: int(transition.FromBucketID), To: int(transition.ToBucketID)}
		if snap != nil {
			for _, guard := range snap.Guards(transition.FromBucketID, transition.ToBucketID) {
				item.Guards = append(item.Guards, ConfigGuard(guard))
			}
		}
		out.Transitions = append(out.Transitions, item)
	}
	operations := workflow.Operations
	if snap != nil {
		operations = snap.Operations()
	}
	out.Operations = config.WorkflowOperations{
		Archive:   config.OperationPolicy{Guards: ConfigGuards(operations.Archive.Guards)},
		Delete:    config.OperationPolicy{Guards: ConfigGuards(operations.Delete.Guards)},
		Unarchive: config.OperationPolicy{Guards: ConfigGuards(operations.Unarchive.Guards)},
	}
	return out
}

// ConfigGuard converts one domain guard without importing UI policy.
func ConfigGuard(guard domain.TransitionGuard) config.TransitionGuard {
	return config.TransitionGuard{Type: guard.Type, Buckets: append([]string(nil), guard.Buckets...), Count: guard.Count, Tag: guard.Tag, Hint: guard.Hint}
}

// ConfigGuards converts domain guards while preserving declaration order.
func ConfigGuards(guards []domain.TransitionGuard) []config.TransitionGuard {
	out := make([]config.TransitionGuard, len(guards))
	for i, guard := range guards {
		out[i] = ConfigGuard(guard)
	}
	return out
}

// WorkflowRowKind identifies a row in the workflow inspector list.
type WorkflowRowKind uint8

const (
	WorkflowBucket WorkflowRowKind = iota
	WorkflowGuard
	WorkflowOpen
	WorkflowAdd
	WorkflowOperation
)

// WorkflowRow is a semantic row; screen code only chooses styling and layout.
type WorkflowRow struct {
	Kind       WorkflowRowKind
	Left       string
	Right      string
	Bucket     config.Bucket
	To         config.Bucket
	Guard      config.TransitionGuard
	GuardIndex int
	OpKind     bundledraft.GuardSetKind
	Mute       bool
}

// WorkflowRows projects buckets, transitions, guards, and operation guards in
// deterministic display order.
func WorkflowRows(workflow config.Workflow, counts map[string]int, text Text) []WorkflowRow {
	buckets := bundledraft.OrderedBuckets(workflow.Buckets)
	if len(buckets) == 0 {
		return nil
	}
	if counts == nil {
		counts = map[string]int{}
	}
	byID := make(map[int]config.Bucket, len(buckets))
	for _, bucket := range buckets {
		byID[bucket.ID] = bucket
	}
	outbound := make(map[int][]config.Transition)
	for _, transition := range workflow.Transitions {
		outbound[transition.From] = append(outbound[transition.From], transition)
	}
	final := buckets[len(buckets)-1].Key
	rows := workflowBucketRows(buckets, byID, outbound, final, counts, text)
	rows = append(rows, WorkflowRow{Kind: WorkflowAdd, Left: "     " + translate(text, "tui.studio.workflow.add_transition", "+ transition")})
	rows = append(rows, workflowOperationRows(workflow, len(buckets)+1, text)...)
	return rows
}

func workflowBucketRows(buckets []config.Bucket, byID map[int]config.Bucket, outbound map[int][]config.Transition, final string, counts map[string]int, text Text) []WorkflowRow {
	rows := make([]WorkflowRow, 0, len(buckets)*4)
	for _, bucket := range buckets {
		left := fmt.Sprintf("%02d // %s · %d", bucket.Position, strings.ToUpper(bucket.Name), counts[bucket.Key])
		if bucket.Key == final {
			left += translate(text, "tui.studio.workflow.final_marker", " · final")
		}
		rows = append(rows, WorkflowRow{Kind: WorkflowBucket, Left: left, Bucket: bucket, Mute: bucket.Key == final})
		for _, transition := range outbound[bucket.ID] {
			rows = append(rows, workflowTransitionRows(bucket, byID[transition.To], transition, text)...)
		}
	}
	return rows
}

func workflowTransitionRows(from, to config.Bucket, transition config.Transition, text Text) []WorkflowRow {
	arrow := translate(text, "tui.studio.workflow.arrow", "-> %s", to.Key)
	if len(transition.Guards) == 0 {
		return []WorkflowRow{{Kind: WorkflowOpen, Left: "     " + translate(text, "tui.studio.workflow.open", "open"), Right: arrow, Bucket: from, To: to}}
	}
	rows := make([]WorkflowRow, 0, len(transition.Guards))
	for i, guard := range transition.Guards {
		rows = append(rows, WorkflowRow{Kind: WorkflowGuard, Left: "     " + GuardLabel(guard), Right: arrow, Bucket: from, To: to, Guard: guard, GuardIndex: i})
	}
	return rows
}

func workflowOperationRows(workflow config.Workflow, operationNumber int, text Text) []WorkflowRow {
	rows := make([]WorkflowRow, 0)
	operations := []struct {
		kind   bundledraft.GuardSetKind
		label  string
		guards []config.TransitionGuard
	}{
		{bundledraft.GuardSetArchive, translate(text, "tui.studio.workflow.op.archive", "archive"), workflow.Operations.Archive.Guards},
		{bundledraft.GuardSetDelete, translate(text, "tui.studio.workflow.op.delete", "delete"), workflow.Operations.Delete.Guards},
		{bundledraft.GuardSetUnarchive, translate(text, "tui.studio.workflow.op.unarchive", "unarchive"), workflow.Operations.Unarchive.Guards},
	}
	for _, operation := range operations {
		if operation.kind != bundledraft.GuardSetArchive && len(operation.guards) == 0 {
			continue
		}
		first := config.TransitionGuard{}
		if len(operation.guards) > 0 {
			first = operation.guards[0]
		}
		rows = append(rows, WorkflowRow{Kind: WorkflowOperation, Left: fmt.Sprintf("%02d // %s", operationNumber, operation.label), Right: GuardLabel(first), OpKind: operation.kind, Guard: first})
		for i := 1; i < len(operation.guards); i++ {
			rows = append(rows, WorkflowRow{Kind: WorkflowGuard, Left: "     " + GuardLabel(operation.guards[i]), OpKind: operation.kind, Guard: operation.guards[i], GuardIndex: i})
		}
		operationNumber++
	}
	return rows
}

// GuardLabel returns the compact label used in workflow rows.
func GuardLabel(guard config.TransitionGuard) string {
	if guard.Type == "comments_tagged" && guard.Tag != "" {
		return "#" + guard.Tag
	}
	if guard.Type != "" {
		return guard.Type
	}
	return "guard"
}

// FlowWarnings performs reachability checks with breadth-first traversal.
func FlowWarnings(workflow config.Workflow, text Text) []string {
	buckets := bundledraft.OrderedBuckets(workflow.Buckets)
	if len(buckets) == 0 {
		return nil
	}
	final := buckets[len(buckets)-1]
	outbound := make(map[int]int)
	graph := make(map[int][]int)
	reopen := 0
	for _, transition := range workflow.Transitions {
		outbound[transition.From]++
		graph[transition.From] = append(graph[transition.From], transition.To)
		if transition.From == final.ID && transition.To != final.ID {
			reopen++
		}
	}
	var warnings []string
	for _, bucket := range buckets[:len(buckets)-1] {
		if outbound[bucket.ID] == 0 {
			warnings = append(warnings, translate(text, "tui.studio.flow.warn.no_outbound", "non-final bucket %s has no outbound transitions", bucket.Key))
		}
	}
	if reopen == 0 {
		warnings = append(warnings, translate(text, "tui.studio.flow.warn.no_reopen", "final bucket %s has no regression/reopen transitions", final.Key))
	}
	if !pathExists(graph, buckets[0].ID, final.ID) {
		warnings = append(warnings, translate(text, "tui.studio.flow.warn.no_path", "no path from %s to final bucket %s", buckets[0].Key, final.Key))
	}
	return warnings
}

func pathExists(graph map[int][]int, from, to int) bool {
	if from == to {
		return true
	}
	seen := map[int]bool{from: true}
	queue := []int{from}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range graph[current] {
			if next == to {
				return true
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

// FirstMissingTransition selects the first absent directed edge in workflow
// order, which keeps the editor's add action deterministic.
func FirstMissingTransition(workflow config.Workflow, buckets []config.Bucket) (fromID, toID int, ok bool) {
	existing := bundledraft.TransitionSet(workflow.Transitions)
	for _, from := range buckets {
		for _, to := range buckets {
			if from.ID == to.ID {
				continue
			}
			if _, present := existing[bundledraft.TransitionKey(config.Transition{From: from.ID, To: to.ID})]; !present {
				return from.ID, to.ID, true
			}
		}
	}
	return 0, 0, false
}

// WorkflowPermissionAllowed resolves bucket → workflow default → allow.
func WorkflowPermissionAllowed(bucket config.Bucket, defaults *config.WorkflowDefaults, entity, operation string) (allowed, explicit bool) {
	if policy, ok := entityOperation(bucket.Permissions, entity, operation); ok {
		return policyAllowed(policy), true
	}
	if defaults != nil {
		perms := &config.BucketPermissions{Task: defaults.Task, Comment: defaults.Comment}
		if policy, ok := entityOperation(perms, entity, operation); ok {
			return policyAllowed(policy), false
		}
	}
	return true, false
}

func entityOperation(perms *config.BucketPermissions, entity, operation string) (*config.CommentOpPolicy, bool) {
	if perms == nil {
		return nil, false
	}
	var permission *config.EntityPermission
	if entity == "comment" {
		permission = perms.Comment
	} else {
		permission = perms.Task
	}
	if permission == nil {
		return nil, false
	}
	var policy *config.CommentOpPolicy
	switch operation {
	case "create":
		policy = permission.Create
	case "edit":
		policy = permission.Edit
	case "delete":
		policy = permission.Delete
	}
	return policy, policy != nil
}

func policyAllowed(policy *config.CommentOpPolicy) bool {
	return policy == nil || policy.Allow == nil || *policy.Allow
}

// TaskCounts groups the supplied task snapshot by bucket key.
func TaskCounts(tasks []domain.Task) map[string]int {
	counts := make(map[string]int)
	for _, task := range tasks {
		if task.BucketKey != "" {
			counts[task.BucketKey]++
		}
	}
	return counts
}

// CommandRow is one registered or configured command entry.
type CommandRow struct {
	Key         string
	Known       bool
	Description string
	Spec        config.CommandSpec
}

func commandRows(commands map[string]config.CommandSpec, names []string) []CommandRow {
	if len(names) == 0 {
		for name := range commands {
			if name != config.CommandsGlobalKey {
				names = append(names, name)
			}
		}
		sort.Strings(names)
	}
	known := make(map[string]bool, len(names))
	rows := make([]CommandRow, 0, len(names)+len(commands))
	for _, name := range names {
		known[name] = true
		rows = append(rows, CommandRow{Key: name, Known: true, Spec: commands[name]})
	}
	var extra []string
	for name := range commands {
		if name != config.CommandsGlobalKey && !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		rows = append(rows, CommandRow{Key: name, Spec: commands[name]})
	}
	return rows
}

// CommandRows exposes deterministic command catalog projection.
func CommandRows(commands map[string]config.CommandSpec, names []string) []CommandRow {
	return commandRows(commands, names)
}

// CommandIndexFor returns the row index of key, or zero.
func CommandIndexFor(commands map[string]config.CommandSpec, key string) int {
	for i, row := range commandRows(commands, nil) {
		if row.Key == key {
			return i
		}
	}
	return 0
}

// CommandWarnings validates command references against the loaded catalogs.
func CommandWarnings(bundle config.Bundle, text Text) []string {
	known := map[string]struct{}{config.CommandsGlobalKey: {}}
	for _, skill := range bundle.Skills {
		if skill.Command != nil {
			known[skill.Command.Name] = struct{}{}
		}
	}
	personas, skills, laws, templates := slugSet(PersonaSlugs(bundle)), slugSet(SkillSlugs(bundle)), slugSet(LawSlugs(bundle)), slugSet(TemplateSlugs(bundle))
	var warnings []string
	for key, spec := range bundle.Commands {
		warnings = append(warnings, commandWarnings(key, spec, known, personas, skills, laws, templates, text)...)
	}
	sort.Strings(warnings)
	return warnings
}

func commandWarnings(key string, spec config.CommandSpec, known, personas, skills, laws, templates map[string]struct{}, text Text) []string {
	var warnings []string
	if _, ok := known[key]; !ok {
		warnings = append(warnings, translate(text, "tui.studio.commands.warn.unknown_command", "commands.%s: unknown agent command", key))
	}
	if key != config.CommandsGlobalKey && spec.Persona != "" {
		if _, ok := personas[spec.Persona]; !ok {
			warnings = append(warnings, translate(text, "tui.studio.commands.warn.missing_persona", "commands.%s.persona: missing persona %q", key, spec.Persona))
		}
	}
	for _, ref := range spec.Skills {
		if _, ok := skills[ref]; !ok {
			warnings = append(warnings, translate(text, "tui.studio.commands.warn.missing_skill", "commands.%s.skills: missing skill %q", key, ref))
		}
	}
	for _, ref := range append(append([]string(nil), spec.Laws...), spec.LawsDisabled...) {
		if _, ok := laws[ref]; !ok {
			warnings = append(warnings, translate(text, "tui.studio.commands.warn.missing_law", "commands.%s.laws: missing law %q", key, ref))
		}
	}
	for _, ref := range spec.Templates {
		if _, ok := templates[ref]; !ok {
			warnings = append(warnings, translate(text, "tui.studio.commands.warn.missing_template", "commands.%s.templates: missing template %q", key, ref))
		}
	}
	return warnings
}

func PersonaSlugs(bundle config.Bundle) []string {
	items := bundledraft.LoadedPersonas(bundle)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

func SkillSlugs(bundle config.Bundle) []string {
	items := bundledraft.LoadedSkills(bundle)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

func LawSlugs(bundle config.Bundle) []string {
	items := bundledraft.LoadedLaws(bundle)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

func TemplateSlugs(bundle config.Bundle) []string {
	items := bundledraft.LoadedTemplates(bundle)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

// PersonaRow contains a persona and all command relationships for it.
type PersonaRow struct {
	Persona  config.Persona
	Commands []PersonaCommand
}

type PersonaCommand struct {
	Key   string
	Known bool
	Spec  config.CommandSpec
}

// RelatedKind identifies a persona relationship row.
type RelatedKind uint8

const (
	RelatedEmpty RelatedKind = iota
	RelatedSkill
	RelatedLaw
	RelatedCommand
)

// RelatedRow is a normalized persona relationship table row.
type RelatedRow struct {
	Kind   RelatedKind
	Value  string
	Name   string
	Type   string
	Detail string
}

func personaRows(bundle config.Bundle, known []string) []PersonaRow {
	roster := bundle.Personas
	if len(roster) == 0 {
		roster = bundledraft.LoadedPersonas(bundle)
	}
	if len(known) == 0 {
		for _, row := range commandRows(bundle.Commands, nil) {
			known = append(known, row.Key)
		}
	}
	out := make([]PersonaRow, 0, len(roster))
	for _, persona := range roster {
		out = append(out, PersonaRow{Persona: persona, Commands: personaCommands(bundle.Commands, persona.Slug, known)})
	}
	return out
}

func personaCommands(commands map[string]config.CommandSpec, slug string, known []string) []PersonaCommand {
	var out []PersonaCommand
	seen := map[string]bool{}
	for _, key := range known {
		spec, ok := commands[key]
		if ok && spec.Persona == slug {
			out = append(out, PersonaCommand{Key: key, Known: true, Spec: spec})
			seen[key] = true
		}
	}
	var extra []string
	for key, spec := range commands {
		if key != config.CommandsGlobalKey && spec.Persona == slug && !seen[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		out = append(out, PersonaCommand{Key: key, Spec: commands[key]})
	}
	return out
}

// PersonaRows exposes persona relationship projection.
func PersonaRows(bundle config.Bundle, known []string) []PersonaRow {
	return personaRows(bundle, known)
}

// PersonaRoster returns the loaded persona entities in bundle order.
func PersonaRoster(bundle config.Bundle) []config.Persona {
	if len(bundle.Personas) > 0 {
		return append([]config.Persona(nil), bundle.Personas...)
	}
	return append([]config.Persona(nil), bundledraft.LoadedPersonas(bundle)...)
}

// PersonaIndexFor returns the roster index of slug, or zero.
func PersonaIndexFor(bundle config.Bundle, slug string) int {
	for i, row := range personaRows(bundle, nil) {
		if row.Persona.Slug == slug {
			return i
		}
	}
	return 0
}

// PersonaRelatedRows projects skills, laws, and bound commands in stable order.
func PersonaRelatedRows(row PersonaRow, text Text, lawSeverities map[string]string) []RelatedRow {
	repertoire := PersonaSkillList(row.Persona)
	out := make([]RelatedRow, 0, len(repertoire)+len(row.Persona.Laws)+len(row.Commands)+3)
	if len(repertoire) == 0 {
		out = append(out, RelatedRow{Kind: RelatedEmpty, Name: translate(text, "tui.studio.personas.related.skills_empty", "No skills in this repertoire.")})
	} else {
		for _, slug := range repertoire {
			out = append(out, RelatedRow{Kind: RelatedSkill, Value: slug, Name: slug, Type: translate(text, "tui.studio.personas.related.kind.skill", "skill")})
		}
	}
	for _, slug := range row.Persona.Laws {
		out = append(out, RelatedRow{Kind: RelatedLaw, Value: slug, Name: slug, Type: translate(text, "tui.studio.personas.related.kind.law", "law"), Detail: valueOrDash(lawSeverities[slug])})
	}
	if len(row.Commands) == 0 {
		out = append(out, RelatedRow{Kind: RelatedEmpty, Name: translate(text, "tui.studio.personas.related.commands_empty", "No commands bind this persona.")})
	} else {
		for _, command := range row.Commands {
			known := translate(text, "tui.studio.personas.unknown", "unknown")
			if command.Known {
				known = translate(text, "tui.studio.personas.known", "known")
			}
			detail := translate(text, "tui.studio.personas.skills_ratio", "skills %d/%d", len(command.Spec.Skills), len(repertoire))
			out = append(out, RelatedRow{Kind: RelatedCommand, Value: command.Key, Name: command.Key, Type: translate(text, "tui.studio.personas.related.kind.command", "cmd"), Detail: fmt.Sprintf("%s  (%s)", detail, known)})
		}
	}
	return out
}

func PersonaSkillList(persona config.Persona) []string {
	return append([]string(nil), persona.SkillRepertoire...)
}

// PersonaSkillRepertoire returns the selectable skills for a persona slug.
func PersonaSkillRepertoire(bundle config.Bundle, slug string) []string {
	for _, persona := range PersonaRoster(bundle) {
		if persona.Slug == slug {
			return PersonaSkillList(persona)
		}
	}
	return nil
}

// PersonaRole infers the display role from the shipped mapping or description.
func PersonaRole(persona config.Persona) string {
	if role := personaRoleMap[persona.Slug]; role != "" {
		return role
	}
	description := strings.TrimSpace(persona.Description)
	cut := strings.Index(description, "—")
	if cut < 0 {
		cut = strings.Index(description, " - ")
	}
	if cut < 0 {
		return "-"
	}
	role := strings.TrimSpace(description[:cut])
	if role == "" {
		return "-"
	}
	runes := []rune(strings.ToLower(role))
	for i, r := range runes {
		if unicode.IsLetter(r) {
			runes[i] = unicode.ToLower(r)
		}
	}
	return string(runes)
}

var personaRoleMap = map[string]string{
	"kakashi-hatake": "reviewer", "shikamaru-nara": "concierge", "third-hokage": "owner", "konohamaru-class": "ideator", "naruto-uzumaki": "builder", "sakura-haruno": "tester", "iruka-umino": "committer",
}

func slugSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func translate(text Text, key, fallback string, args ...any) string {
	if text != nil {
		return text(key, fallback, args...)
	}
	return fmt.Sprintf(fallback, args...)
}

func cloneHistory(history map[int][]HookExecuted) map[int][]HookExecuted {
	if len(history) == 0 {
		return nil
	}
	out := make(map[int][]HookExecuted, len(history))
	for index, rows := range history {
		out[index] = append([]HookExecuted(nil), rows...)
	}
	return out
}

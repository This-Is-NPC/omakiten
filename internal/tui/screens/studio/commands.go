package studio

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	bundledraft "omakiten/internal/config/bundledraft"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/selectlist"
)

var studioCommandFields = []string{"persona", "skills", "laws", "laws_disabled", "templates"}

// Commands is the first production screengrid body: [screengrid.ListInspector]
// of a command list beside the field table over PREVIEW.
const (
	sectionCommands          = screenlayout.ID("commands")
	sectionCommandsList      = screenlayout.ID("list")
	sectionCommandsInspector = screenlayout.ID("inspector")
	sectionCommandsFields    = screenlayout.ID("fields")
	sectionCommandsPreview   = screenlayout.ID("preview")
)

// studioCommandFieldCount is the metadata table's field count: persona, skills,
// laws, laws_disabled, templates and the global laws. Every command has all six,
// so the zone's height is a constant rather than a function of the selection.
const studioCommandFieldCount = 6

// commandsZones names the inspector's two halves: the command's metadata, and
// the resolved prompt preview under it.
var commandsZones = screenlayout.InspectorIDs{
	Inspector: sectionCommandsInspector, Fields: sectionCommandsFields,
	Detail: sectionCommandsPreview,
}

func (m Screen) commandsRoot() screengrid.Node {
	return screengrid.ListInspector(sectionCommands,
		screengrid.Cell(screenlayout.List(sectionCommandsList), m.commandsListBody),
		screengrid.InspectorColumn(commandsZones,
			screengrid.Cell(commandsZones.FieldsSpec(studioCommandFieldCount), m.commandsFieldsBody),
			screengrid.Cell(commandsZones.DetailSpec(false, 0), m.commandsPreviewBody),
			true, true),
	)
}

func (m Screen) commandsBox() screenlayout.Box {
	return screenlayout.HostBox(m.kit)
}

func (m Screen) commandsGridResult() screengrid.Result {
	return screengrid.Render(m.kit, m.grid, m.commandsBox(), m.commandsRoot())
}

func (m Screen) viewStudioCommands() string {
	view := m.commandsGridResult().View
	if view == "" {
		return screenkit.Indent("\n", 2)
	}
	return screenkit.Indent("\n"+view, 2)
}

func (m Screen) commandsListBody(canvas screenlayout.Canvas) screenlayout.Block {
	held := canvas.Cursor()
	rows := m.projection.CommandRows
	m.studioCommandsClamp(len(rows))
	if held < 0 {
		held = m.studioCommandIndex
	}

	spec := selectlist.Spec{
		Kicker: m.sectionKickerCount(m.tr("tui.studio.commands.kicker", "COMMANDS"), len(rows), m.zoneFocused(sectionCommandsList)),
		Cursor: held,
		Empty:  m.tr("tui.studio.commands.bindings_empty", "No known MCP commands."),
	}
	spec.Width = canvas.Width()
	if len(rows) == 0 {
		block := selectlist.Paint(m.kit, spec)
		block.Cursor = screenlayout.NoSelection()
		return block
	}
	spec.Rows = make([]selectlist.Row, len(rows))
	for i, row := range rows {
		known := m.tr("tui.studio.commands.known", "known")
		if !row.Known {
			known = m.tr("tui.studio.commands.unknown_configured", "unknown configured")
		}
		spec.Rows[i] = selectlist.Row{Left: fmt.Sprintf("%02d // %s (%s)", i+1, screenkit.Sanitize(row.Key), known)}
	}
	return selectlist.Paint(m.kit, spec)
}

// commandsFieldsBody is the view-only zone: the selected command's metadata.
func (m Screen) commandsFieldsBody(canvas screenlayout.Canvas) screenlayout.Block {
	bundle := m.projection.Bundle
	rows := m.projection.CommandRows
	m.studioCommandsClamp(len(rows))
	if len(rows) == 0 {
		return studioFieldsBlock(nil, nil)
	}
	return studioFieldsBlock(m.commandsInspectorTable(rows[m.studioCommandIndex], bundle, canvas.Width(), studioFieldsBudget(canvas, nil)), nil)
}

// commandsPreviewBody is the PREVIEW box under the metadata: the resolved prompt
// plus whatever the draft has to say about it.
func (m Screen) commandsPreviewBody(canvas screenlayout.Canvas) screenlayout.Block {
	_ = canvas.Cursor()
	bundle := m.projection.Bundle
	report := m.studioPreviewReport()
	rows := m.projection.CommandRows
	m.studioCommandsClamp(len(rows))

	var body []string
	if m.studioCommandMsg != "" {
		body = append(body, m.styles.Hint.Render(screenkit.Sanitize(m.studioCommandMsg)))
	}
	if report.ValidationError != nil {
		body = append(body, m.styles.Hint.Render(m.tr("tui.studio.commands.validation", "Validation: %s", screenkit.Sanitize(report.ValidationError.Error()))))
	}
	if warnings := m.projection.CommandWarnings; len(warnings) > 0 {
		body = append(body, m.tr("tui.studio.commands.warnings_header", "Command warnings:"))
		body = append(body, prefixLines(warnings, "- ")...)
	}
	if len(report.DiffSummary) > 0 && report.Dirty {
		body = append(body, m.tr("tui.studio.commands.candidate_diff", "Candidate diff:"))
		body = append(body, prefixLines(report.DiffSummary, "- ")...)
	}
	kicker := ""
	focused := m.zoneFocused(sectionCommandsPreview)
	if len(rows) > 0 {
		body = append(body, splitNonEmpty(m.renderPreparedStudioPromptPreview(rows[m.studioCommandIndex]))...)
		if repertoire := studioprojection.PersonaSkillRepertoire(bundle, rows[m.studioCommandIndex].Spec.Persona); len(repertoire) > 0 {
			body = append(body, m.tr("tui.studio.commands.selectable_skills", "  selectable skills: %s", screenkit.Sanitize(strings.Join(repertoire, ", "))))
		}
		kicker = m.sectionKicker(m.tr("tui.studio.commands.preview_kicker", "PREVIEW"), focused)
	}
	return m.inspectorBox(canvas, kicker, "", body, focused)
}

func (m Screen) commandsInspectorTable(row studioprojection.CommandRow, bundle config.Bundle, width, rows int) []string {
	fields := [][2]string{
		{m.tr("tui.studio.commands.field.persona", "persona"), valueOrDash(row.Spec.Persona)},
		{m.tr("tui.studio.commands.field.skills", "skills"), valueOrDash(strings.Join(row.Spec.Skills, ", "))},
		{m.tr("tui.studio.commands.field.laws", "laws"), valueOrDash(strings.Join(row.Spec.Laws, ", "))},
		{m.tr("tui.studio.commands.field.laws_disabled", "laws_disabled"), valueOrDash(strings.Join(row.Spec.LawsDisabled, ", "))},
		{m.tr("tui.studio.commands.field.templates", "templates"), valueOrDash(strings.Join(row.Spec.Templates, ", "))},
		{m.tr("tui.studio.commands.field.global_laws", "global laws"), valueOrDash(strings.Join(bundle.MCPCommands[config.MCPCommandsGlobalKey].Laws, ", "))},
	}
	table := make([][]gridtable.Cell, 0, len(fields)+1)
	table = append(table, []gridtable.Cell{gridtable.Styled(m.studioFieldsTitle(row.Key, m.zoneFocused(sectionCommandsFields)))})
	for _, field := range fields {
		table = append(table, []gridtable.Cell{gridtable.Raw(field[0]), gridtable.Raw(field[1])})
	}
	return m.studioFieldTable(width, rows, summaryTablesOpts{LabelWidth: 16, ValueWidth: 32, Auto: true}, table)
}

func (m *Screen) handleStudioCommandsKey(msg tea.KeyMsg) tea.Cmd {
	if studioHorizontalNavKey(msg.String()) {
		return nil
	}
	bundle := m.studioCommandsCandidate()
	rows := studioCommandRows(bundle.MCPCommands, m.knownCommandNames())
	if len(rows) == 0 {
		m.handleStudioCommandsGridKey(msg.String())
		return nil
	}
	m.studioCommandsClamp(len(rows))
	row := rows[m.studioCommandIndex]
	spec := row.Spec
	if cmd, handled := m.applyStudioCommandsKey(msg.String(), bundle, row.Key, spec); handled {
		return cmd
	}
	// The mutating keys above changed the CANDIDATE, not the selection, so the
	// grid only needs to re-measure what the new candidate renders to.
	m.resyncCommandsGrid()
	return m.commandPreviewCmd()
}

func (m *Screen) applyStudioCommandsKey(key string, bundle config.Bundle, rowKey string, spec config.MCPCommandSpec) (tea.Cmd, bool) {
	switch key {
	case "up", "k", "down", "j":
		m.handleStudioCommandsGridKey(key)
		return m.commandPreviewCmd(), true
	case "p":
		spec.Persona = nextSlug(spec.Persona, personaSlugs(bundle))
		m.saveStudioCommandSpec(rowKey, spec, m.tr("tui.studio.commands.msg.persona_changed", "persona changed"))
	case "s":
		spec.Skills = toggleNextSlug(spec.Skills, personaSkillRepertoire(bundle, spec.Persona))
		m.saveStudioCommandSpec(rowKey, spec, m.tr("tui.studio.commands.msg.skill_selection_changed", "skill selection changed"))
	case "g":
		global := bundle.MCPCommands[config.MCPCommandsGlobalKey]
		global.Laws = toggleNextSlug(global.Laws, lawSlugs(bundle))
		m.saveStudioCommandSpec(config.MCPCommandsGlobalKey, global, m.tr("tui.studio.commands.msg.global_law_selection_changed", "global law selection changed"))
	case "w":
		spec.Laws = toggleNextSlug(spec.Laws, lawSlugs(bundle))
		m.saveStudioCommandSpec(rowKey, spec, m.tr("tui.studio.commands.msg.law_selection_changed", "law selection changed"))
	case "x":
		spec.LawsDisabled = toggleNextSlug(spec.LawsDisabled, lawSlugs(bundle))
		m.saveStudioCommandSpec(rowKey, spec, m.tr("tui.studio.commands.msg.disabled_law_selection_changed", "disabled-law selection changed"))
	case "t":
		spec.Templates = toggleNextSlug(spec.Templates, templateSlugs(bundle))
		m.saveStudioCommandSpec(rowKey, spec, m.tr("tui.studio.commands.msg.template_selection_changed", "template selection changed"))
	case "d":
		spec = removeLastStudioCommandFieldValue(spec, studioCommandFields[m.studioCommandField])
		m.saveStudioCommandSpec(rowKey, spec, m.tr("tui.studio.commands.msg.field_value_removed", "field value removed"))
	case "ctrl+s":
		if m.studioDraft == nil {
			m.studioCommandMsg = m.tr("tui.studio.commands.msg.no_changes_to_save", "no Studio Commands changes to save")
			return nil, true
		}
		m.openStudioApplyOverlay()
		return nil, true
	default:
		m.handleStudioCommandsGridKey(key)
		return m.commandPreviewCmd(), true
	}
	return nil, false
}

func (m *Screen) handleStudioCommandsGridKey(key string) {
	root := m.commandsRoot()
	box := m.commandsBox()
	next, handled := m.grid.HandleKey(m.kit, box, key, root)
	if !handled {
		return
	}
	m.grid = next
	if cur := m.grid.Layout().Cursor(sectionCommandsList); cur >= 0 {
		m.studioCommandIndex = cur
		m.studioCommandsClamp(len(studioCommandRows(m.studioCandidateCommands(), m.knownCommandNames())))
	}
}

// resyncCommandsGrid seeds the grid with the selection the screen was BOUND with and
// then re-measures.
//
// The grid owns the cursor while the screen is running — every key goes through
// it and the read-back brings the answer out — but the screen is bound with a
// State that may carry a selection (a jump from Personas parks a command index,
// a session restores one). Entry is where the two meet, and it is the only place
// the screen writes a cursor the grid did not decide.
func (m *Screen) resyncCommandsGrid() {
	m.grid = m.grid.WithCursor(sectionCommandsList, m.studioCommandIndex).
		Resync(m.kit, m.commandsBox(), m.commandsRoot())
}

func splitNonEmpty(block string) []string {
	if block == "" {
		return nil
	}
	return strings.Split(block, "\n")
}

func (m *Screen) saveStudioCommandSpec(key string, spec config.MCPCommandSpec, ok string) {
	if err := m.ensureStudioDraft(); err != nil {
		m.studioCommandMsg = err.Error()
		return
	}
	if key == config.MCPCommandsGlobalKey {
		spec.Persona = ""
		spec.Skills = nil
		spec.Templates = nil
		spec.LawsDisabled = nil
	}
	report := m.studioDraft.SetMCPCommandSpec(key, spec)
	m.studioCommandMsg = studioDraftReportMessage(report, ok)
	m.refreshProjection()
}

// studioCommandsCandidate is the bundle alone, for the read-only paint paths. Building the
// report to throw it away is two deep clones and a diff per body — see
// [StudioDraft.candidateBundle].
func (m Screen) studioCommandsCandidate() config.Bundle {
	if m.studioDraft != nil {
		return m.studioDraft.Candidate()
	}
	bundle, _ := m.studioCommandsBundle()
	return bundle
}

func (m Screen) studioCommandsBundle() (config.Bundle, bundledraft.Report) {
	if m.studioDraft != nil {
		report := m.studioDraft.ReportText(m.t)
		return report.Candidate, report
	}
	if snap := m.repos.activeSnapshot(); snap != nil {
		bundle := config.Bundle{MCPCommands: snap.MCPCommands(), AllPersonas: snap.AllPersonas(), AllSkills: snap.AllSkills(), AllLaws: snap.AllLaws(), AllTemplates: snap.AllTemplates()}
		return bundle, bundledraft.Report{Candidate: bundle}
	}
	return config.Bundle{}, bundledraft.Report{}
}

func (m *Screen) studioCommandsClamp(n int) {
	if n <= 0 {
		m.studioCommandIndex, m.studioCommandField = 0, 0
		return
	}
	if m.studioCommandIndex < 0 {
		m.studioCommandIndex = 0
	}
	if m.studioCommandIndex >= n {
		m.studioCommandIndex = n - 1
	}
	if m.studioCommandField < 0 {
		m.studioCommandField = 0
	}
	if m.studioCommandField >= len(studioCommandFields) {
		m.studioCommandField = len(studioCommandFields) - 1
	}
}

func studioCommandRows(commands map[string]config.MCPCommandSpec, names []string) []studioprojection.CommandRow {
	return studioprojection.CommandRows(commands, names)
}

func CommandIndexFor(commands map[string]config.MCPCommandSpec, key string) int {
	return studioprojection.CommandIndexFor(commands, key)
}

func personaSlugs(bundle config.Bundle) []string {
	return studioprojection.PersonaSlugs(bundle)
}

func lawSlugs(bundle config.Bundle) []string {
	return studioprojection.LawSlugs(bundle)
}

func templateSlugs(bundle config.Bundle) []string {
	return studioprojection.TemplateSlugs(bundle)
}

func personaSkillRepertoire(bundle config.Bundle, personaSlug string) []string {
	return studioprojection.PersonaSkillRepertoire(bundle, personaSlug)
}

func nextSlug(current string, options []string) string {
	if len(options) == 0 {
		return current
	}
	for i, option := range options {
		if option == current {
			return options[(i+1)%len(options)]
		}
	}
	return options[0]
}

func toggleNextSlug(selected, options []string) []string {
	if len(options) == 0 {
		return selected
	}
	set := slugSet(selected)
	for _, option := range options {
		if _, ok := set[option]; !ok {
			return append(selected, option)
		}
	}
	return selected[:len(selected)-1]
}

func removeLastStudioCommandFieldValue(spec config.MCPCommandSpec, field string) config.MCPCommandSpec {
	switch field {
	case "persona":
		spec.Persona = ""
	case "skills":
		spec.Skills = trimLast(spec.Skills)
	case "laws":
		spec.Laws = trimLast(spec.Laws)
	case "laws_disabled":
		spec.LawsDisabled = trimLast(spec.LawsDisabled)
	case "templates":
		spec.Templates = trimLast(spec.Templates)
	}
	return spec
}

func trimLast(values []string) []string {
	if len(values) == 0 {
		return values
	}
	return values[:len(values)-1]
}

func slugSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

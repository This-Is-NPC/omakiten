package studio

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/selectlist"
	"omakiten/internal/tui/screenhost"
)

// Personas is the reverse of the Commands inspector: a read-only wiring graph
// of the active kit roster. It never writes personas/*.md — Settings remains
// the file editor. Same ListInspector body as Commands / Workflow.
const (
	sectionPersonas          = screenlayout.ID("personas")
	sectionPersonasList      = screenlayout.ID("list")
	sectionPersonasInspector = screenlayout.ID("inspector")
	sectionPersonasFields    = screenlayout.ID("fields")
	sectionPersonasRelated   = screenlayout.ID("related")
)

// studioPersonaFieldCount is the persona table's field count: slug, name, role,
// schema, laws and source. Every persona has all six.
const studioPersonaFieldCount = 6

type studioPersonaRow = studioprojection.PersonaRow

const (
	relatedEmpty   = studioprojection.RelatedEmpty
	relatedSkill   = studioprojection.RelatedSkill
	relatedLaw     = studioprojection.RelatedLaw
	relatedCommand = studioprojection.RelatedCommand
)

// These kinds match their entitylist counterparts for ActionOpenEntity.
// Studio must not import internal/app (D21); the host maps these strings.
const (
	personaKind             = "personas"
	personaRelatedSkillKind = "skills"
	personaRelatedLawKind   = "laws"
)

// personaRelatedRow is one RELATED entry as DATA — the cells, not a rendered
// line.
//
// It used to carry `Left` and `Right`, already formatted, which is why the three
// kinds could not line up: each built its own two-part string and the widths
// were whatever the text happened to be. Cells let one width fitter size the
// columns for every row, which is the same thing Hooks' HISTORY does.
type personaRelatedRow = studioprojection.RelatedRow

// personasZones names the inspector's two halves: the persona's fields, and the
// RELATED list under them.
var personasZones = screenlayout.InspectorIDs{
	Inspector: sectionPersonasInspector, Fields: sectionPersonasFields,
	Detail: sectionPersonasRelated,
}

func (m Screen) personasRoot() screengrid.Node {
	return screengrid.ListInspector(sectionPersonas,
		screengrid.Cell(screenlayout.List(sectionPersonasList), m.personasListBody),
		screengrid.InspectorColumn(personasZones,
			screengrid.Cell(personasZones.FieldsSpec(studioPersonaFieldCount), m.personasFieldsBody),
			screengrid.Cell(personasZones.DetailSpec(true, personaRelatedHeaderRows), m.personasRelatedBody),
			true, true),
	)
}

func (m Screen) personasBox() screenlayout.Box {
	return screenlayout.HostBox(m.kit)
}

func (m Screen) personasGridResult() screengrid.Result {
	return screengrid.Render(m.kit, m.grid, m.personasBox(), m.personasRoot())
}

func (m Screen) viewStudioPersonas() string {
	view := m.personasGridResult().View
	if view == "" {
		return screenkit.Indent("\n", 2)
	}
	return screenkit.Indent("\n"+view, 2)
}

func (m Screen) personasListBody(canvas screenlayout.Canvas) screenlayout.Block {
	held := canvas.Cursor()
	bundle := m.projection.Bundle
	rows := m.projection.Personas
	m.studioPersonasClamp(len(rows))
	if held < 0 {
		held = m.studioPersonaIndex
	}

	spec := selectlist.Spec{
		Kicker: m.personasListChrome(len(rows), bundle),
		Cursor: held,
		Empty:  m.tr("tui.studio.personas.empty", "No personas wired in this kit."),
	}
	spec.Width = canvas.Width()
	if len(rows) == 0 {
		block := selectlist.Paint(m.kit, spec)
		block.Cursor = screenlayout.NoSelection()
		return block
	}
	spec.Rows = make([]selectlist.Row, len(rows))
	for i, row := range rows {
		spec.Rows[i] = selectlist.Row{
			Left:  fmt.Sprintf("%02d // %s", i+1, screenkit.Sanitize(row.Persona.Slug)),
			Right: m.tr("tui.studio.personas.cmd_count", "%d cmd", len(row.Commands)),
		}
	}
	return selectlist.Paint(m.kit, spec)
}

func (m Screen) personasListChrome(n int, bundle config.Bundle) string {
	kicker := m.sectionKicker(m.tr("tui.studio.personas.kicker", "PERSONAS"), m.zoneFocused(sectionPersonasList))
	trail := m.tr("tui.studio.personas.summary", "%d  ·  %s roster", n, studioPersonaRosterLabel(bundle))
	return kicker + m.styles.Hint.Render(" · "+trail)
}

// personasFieldsBody is the view-only zone: the selected persona's fields, with
// the screen message as unscrolled footer chrome under them.
func (m Screen) personasFieldsBody(canvas screenlayout.Canvas) screenlayout.Block {
	rows := m.projection.Personas
	m.studioPersonasClamp(len(rows))
	var footer []string
	if m.studioPersonaMsg != "" {
		footer = []string{m.styles.Hint.Render(screenkit.Sanitize(m.studioPersonaMsg))}
	}
	var table []string
	if len(rows) > 0 {
		table = m.personasFieldsTable(rows[m.studioPersonaIndex], canvas.Width(), studioFieldsBudget(canvas, footer))
	}
	return studioFieldsBlock(table, footer)
}

// personasRelatedBody is the RELATED box: the one Studio detail zone that is a
// LIST rather than a read-only body, so it keeps its own cursor.
func (m Screen) personasRelatedBody(canvas screenlayout.Canvas) screenlayout.Block {
	held := canvas.Cursor()
	rows := m.projection.Personas
	m.studioPersonasClamp(len(rows))
	if len(rows) == 0 {
		return screenlayout.Block{Cursor: screenlayout.NoSelection()}
	}

	kicker, related := m.personasRelatedContent(rows[m.studioPersonaIndex])
	m.studioPersonasRelatedClamp(len(related))
	cursor := held
	if cursor < 0 {
		cursor = m.studioPersonaRelatedIndex
	}
	widths := personaRelatedWidths(selectlist.InnerWidth(canvas.Width()))
	spec := selectlist.Spec{
		Kicker:       kicker,
		ColumnHeader: m.personaRelatedHeader(widths),
		Cursor:       cursor,
		Empty:        screenkit.Sanitize(m.tr("tui.studio.personas.related.skills_empty", "No skills in this repertoire.")),
	}
	empty := len(related) == 0
	if !empty {
		spec.Rows = make([]selectlist.Row, len(related))
		for i, row := range related {
			spec.Rows[i] = selectlist.Row{Left: personaRelatedLine(i, row, widths)}
		}
	}
	spec.Width = canvas.Width()
	block := selectlist.Paint(m.kit, spec)
	if empty {
		block.Cursor = screenlayout.NoSelection()
	}
	return block
}

// The RELATED table's columns: the row number, the slug, what KIND of thing it
// is, and whatever that kind has to say about itself.
//
// It is the table Hooks' HISTORY paints — [gridtable.FitWidths] to size the
// columns against the width actually available, then one [gridtable.FormatRow]
// per line — rather than a second alignment scheme. Before this the rows carried
// a left half and a right half and nothing lined up between the three kinds; the
// kind itself was only legible because skills happened to be listed first.
// The NAME column is declared far wider than it will ever get, and that is how
// it becomes the one that absorbs the width. FitWidths shrinks the WIDEST column
// each round, so a column that starts widest keeps taking the cuts until it is
// no longer widest — which means the two columns with a known maximum ("command"
// is the longest type, "skills 12/24  (unknown)" the longest detail) hold their
// size and the slug column gets everything that is left.
var personaRelatedNaturalWidths = []int{4, 60, 7, 23}

const (
	// personaRelatedMarkWidth is the cursor gutter selectlist prints before
	// every row. The header is not a row and does not get one, so it is indented
	// by hand — otherwise every column heading sits two columns left of the
	// column it names.
	personaRelatedMarkWidth = 2
	personaRelatedColMin    = 4
	// personaRelatedHeaderRows is the column-header line this table pins above
	// its rows, which the zone's floor has to pay for on top of a box's own.
	personaRelatedHeaderRows = 1
)

func personaRelatedWidths(inner int) []int {
	gaps := len(personaRelatedNaturalWidths) - 1
	return gridtable.FitWidths(personaRelatedNaturalWidths, inner-personaRelatedMarkWidth-gaps, personaRelatedColMin)
}

// personaRelatedHeader is the column heading, painted below the rule where
// Hooks' HISTORY paints its own, in the same Info style.
//
// The gutter indent is the one difference the two tables cannot share: HISTORY
// has no cursor, so its rows start at the box edge, and RELATED's start two
// columns in behind the ›. The heading follows the columns it names.
func (m Screen) personaRelatedHeader(widths []int) string {
	return strings.Repeat(" ", personaRelatedMarkWidth) + m.styles.Info.Render(gridtable.FormatRow([]string{
		"",
		m.tr("tui.studio.personas.related.col.name", "NAME"),
		m.tr("tui.log.col.type", "TYPE"),
		m.tr("tui.log.col.detail", "DETAIL"),
	}, widths))
}

// personaRelatedLine is one table row. An EMPTY row is copy, not data — it spans
// the table rather than being squeezed into the name column.
func personaRelatedLine(index int, row personaRelatedRow, widths []int) string {
	name := screenkit.Sanitize(row.Name)
	if row.Kind == relatedEmpty {
		return name
	}
	return gridtable.FormatRow([]string{
		fmt.Sprintf("%02d", index+1), name, screenkit.Sanitize(row.Type), screenkit.Sanitize(row.Detail),
	}, widths)
}

func (m Screen) personasFieldsTable(row studioPersonaRow, width, rows int) []string {
	persona := row.Persona
	repertoire := studioprojection.PersonaSkillList(persona)
	fields := [][2]string{
		{m.tr("tui.studio.personas.field.slug", "slug"), screenkit.Sanitize(persona.Slug)},
		{m.tr("tui.studio.personas.field.name", "name"), screenkit.Sanitize(valueOrDash(persona.Name))},
		{m.tr("tui.studio.personas.field.role", "role"), screenkit.Sanitize(studioprojection.PersonaRole(persona))},
		{m.tr("tui.studio.personas.field.schema", "schema"), personaSchemaLabel(persona)},
		{m.tr("tui.studio.personas.field.laws", "laws"), screenkit.Sanitize(valueOrDash(strings.Join(persona.Laws, ", ")))},
		{m.tr("tui.studio.personas.field.source", "source"), screenkit.Sanitize(personaSourceLabel(persona))},
	}
	title := fmt.Sprintf("%02d // %s", m.studioPersonaIndex+1, screenkit.Sanitize(persona.Slug))
	cells := m.studioFieldRows(title, m.zoneFocused(sectionPersonasFields), fields...)
	_ = repertoire
	return m.studioFieldTable(width, rows, summaryTablesOpts{LabelWidth: 12, ValueWidth: 40, Auto: true}, cells)
}

func (m Screen) personasRelatedContent(row studioPersonaRow) (kicker string, related []personaRelatedRow) {
	persona := row.Persona
	repertoire := studioprojection.PersonaSkillList(persona)
	kicker = m.sectionKicker(m.tr("tui.studio.personas.related.heading", "RELATED"), m.zoneFocused(sectionPersonasRelated)) + m.styles.Hint.Render(" · "+m.tr(
		"tui.studio.personas.related.kicker",
		"%d skills · %d laws · %d commands",
		len(repertoire), len(persona.Laws), len(row.Commands),
	))
	return kicker, m.personaRelatedRows(row)
}

// personaRelatedRows is everything the persona reaches: its skills, its laws and
// the commands bound to it, in that order.
//
// The LAWS were missing. The kicker has counted them since this screen shipped
// ("%d skills · %d laws · %d commands") and the list below it never held one, so
// the header advertised a third group with nothing behind it. A type column is
// what makes three groups legible in one table, and once there is a type column
// there is no reason left to leave a group out.
func (m Screen) personaRelatedRows(row studioPersonaRow) []personaRelatedRow {
	lawSeverities := m.projection.LawSeverities
	if lawSeverities == nil && !m.projectionReady {
		lawSeverities = studioprojection.Build(studioprojection.Input{Bundle: m.studioPersonasCandidate()}).LawSeverities
	}
	return studioprojection.PersonaRelatedRows(row, projectionText(m.t), lawSeverities)
}

// personaLawSeverities is the severity of every law the candidate knows, keyed
// by slug, so a law row can say what it is without a second lookup per row.
func (m *Screen) handleStudioPersonasKey(msg tea.KeyMsg) screenhost.Outcome {
	if studioHorizontalNavKey(msg.String()) {
		return screenhost.Stay(*m, nil)
	}
	if msg.String() == "ctrl+s" {
		if m.studioDraft == nil {
			m.studioPersonaMsg = m.tr("tui.studio.personas.msg.no_changes_to_save", "no Studio Personas changes to save")
			return screenhost.Stay(*m, nil)
		}
		m.openStudioApplyOverlay()
		return screenhost.Stay(*m, nil)
	}
	bundle := m.studioPersonasCandidate()
	rows := studioPersonaRows(bundle, m.knownCommandNames())
	if len(rows) == 0 {
		m.handleStudioPersonasGridKey(msg.String())
		return screenhost.Stay(*m, nil)
	}
	m.studioPersonasClamp(len(rows))
	related := m.personaRelatedRows(rows[m.studioPersonaIndex])
	m.studioPersonasRelatedClamp(len(related))
	focus := m.grid.Focus()
	switch msg.String() {
	case "up", "k", "down", "j":
		// One owner for the cursor: the GRID. It routes the key to the focused
		// zone — the list's selection, the RELATED list's selection, or the
		// view-only fields zone's offset — and the read-back in
		// handleStudioPersonasGridKey brings the answer into the screen's own
		// index.
		//
		// This used to be a three-way switch that moved the index by hand and then
		// re-synced the whole grid to tell it what had happened. That resync was a
		// full re-measure of every zone on every keystroke, and it was more than
		// half the cost of scrolling the RELATED list.
		m.handleStudioPersonasGridKey(msg.String())
		return screenhost.Stay(*m, nil)
	case "enter":
		switch focus {
		case sectionPersonasList:
			return screenhost.Outcome{Screen: *m, Action: screenhost.Action{
				Kind:       screenhost.ActionOpenEntity,
				EntityKind: personaKind,
				Value:      rows[m.studioPersonaIndex].Persona.Slug,
			}}
		case sectionPersonasRelated:
			return m.openPersonaRelated(related)
		default:
			return screenhost.Stay(*m, nil)
		}
	default:
		m.handleStudioPersonasGridKey(msg.String())
		return screenhost.Stay(*m, nil)
	}
}

// openPersonaRelated is the Settings-style preview jump from a related row.
// Skills emit ActionOpenEntity with EntityKind "skills" (entitylist.KindSkills);
// the host stacks entitydetail. Commands have no entitydetail kind — enter
// parks studioCommandIndex on that MCP key and returns Navigate(StudioCommands).
// The host stores this Screen (one value for every Studio sub) before
// navigateToScreen, so Bind(StudioCommands) reuses the parked index.
func (m *Screen) openPersonaRelated(related []personaRelatedRow) screenhost.Outcome {
	if m.studioPersonaRelatedIndex < 0 || m.studioPersonaRelatedIndex >= len(related) {
		return screenhost.Stay(*m, nil)
	}
	row := related[m.studioPersonaRelatedIndex]
	switch row.Kind {
	case relatedSkill, relatedLaw:
		kind := personaRelatedSkillKind
		if row.Kind == relatedLaw {
			kind = personaRelatedLawKind
		}
		return screenhost.Outcome{Screen: *m, Action: screenhost.Action{
			Kind:       screenhost.ActionOpenEntity,
			EntityKind: kind,
			Value:      row.Value,
		}}
	case relatedCommand:
		bundle := m.studioPersonasCandidate()
		m.studioCommandIndex = CommandIndexFor(bundle.MCPCommands, row.Value)
		m.studioCommandsClamp(len(studioCommandRows(bundle.MCPCommands, m.knownCommandNames())))
		// list IDs are shared ("list") across Studio subs; pin the cursor so
		// Bind(StudioCommands) paints this key, not the leftover persona index.
		m.grid = m.grid.WithCursor(sectionCommandsList, m.studioCommandIndex)
		return screenhost.Navigate(*m, screenhost.StudioCommands, nil)
	default:
		return screenhost.Stay(*m, nil)
	}
}

func (m *Screen) handleStudioPersonasGridKey(key string) {
	root := m.personasRoot()
	box := m.personasBox()
	next, handled := m.grid.HandleKey(m.kit, box, key, root)
	if !handled {
		return
	}
	m.grid = next
	if cur := m.grid.Layout().Cursor(sectionPersonasList); cur >= 0 {
		if cur != m.studioPersonaIndex {
			m.studioPersonaRelatedIndex = 0
		}
		m.studioPersonaIndex = cur
		m.studioPersonasClamp(len(studioPersonaRows(m.studioPersonasOnly(), m.knownCommandNames())))
	}
	if cur := m.grid.Layout().Cursor(sectionPersonasRelated); cur >= 0 {
		m.studioPersonaRelatedIndex = cur
		bundle := m.studioPersonasCandidate()
		rows := studioPersonaRows(bundle, m.knownCommandNames())
		if len(rows) > 0 {
			m.studioPersonasRelatedClamp(len(m.personaRelatedRows(rows[m.studioPersonaIndex])))
		}
	}
}

// resyncPersonasGrid seeds the grid with the selection the screen was BOUND with and
// then re-measures.
//
// The grid owns the cursor while the screen is running — every key goes through
// it and the read-back brings the answer out — but the screen is bound with a
// State that may carry a selection (a jump from Personas parks a command index,
// a session restores one). Entry is where the two meet, and it is the only place
// the screen writes a cursor the grid did not decide.
func (m *Screen) resyncPersonasGrid() {
	m.grid = m.grid.WithCursor(sectionPersonasList, m.studioPersonaIndex).
		WithCursor(sectionPersonasRelated, m.studioPersonaRelatedIndex).
		Resync(m.kit, m.personasBox(), m.personasRoot())
}

func (m Screen) studioPersonasOnly() config.Bundle {
	bundle := m.studioPersonasCandidate()
	return bundle
}

// studioPersonasCandidate is the bundle alone, for the read-only paint paths. Building the
// report to throw it away is two deep clones and a diff per body — see
// [StudioDraft.candidateBundle].
func (m Screen) studioPersonasCandidate() config.Bundle {
	if m.studioDraft != nil {
		return m.studioDraft.Candidate()
	}
	bundle, _ := m.studioPersonasBundle()
	return bundle
}

func (m Screen) studioPersonasBundle() (config.Bundle, StudioDraftReport) {
	if m.studioDraft != nil {
		report := m.studioDraft.ReportText(m.t)
		return report.Candidate, report
	}
	if snap := m.repos.activeSnapshot(); snap != nil {
		bundle := config.Bundle{
			Kit:          snap.Kit(),
			Personas:     snap.Personas(),
			AllPersonas:  snap.AllPersonas(),
			MCPCommands:  snap.MCPCommands(),
			AllSkills:    snap.AllSkills(),
			Skills:       snap.Skills(),
			AllLaws:      snap.AllLaws(),
			Laws:         snap.Laws(),
			AllTemplates: snap.AllTemplates(),
			Templates:    snap.Templates(),
		}
		return bundle, StudioDraftReport{Candidate: bundle}
	}
	return config.Bundle{}, StudioDraftReport{}
}

func (m *Screen) studioPersonasClamp(n int) {
	if n <= 0 {
		m.studioPersonaIndex = 0
		return
	}
	if m.studioPersonaIndex < 0 {
		m.studioPersonaIndex = 0
	}
	if m.studioPersonaIndex >= n {
		m.studioPersonaIndex = n - 1
	}
}

func (m *Screen) studioPersonasRelatedClamp(n int) {
	if n <= 0 {
		m.studioPersonaRelatedIndex = 0
		return
	}
	if m.studioPersonaRelatedIndex < 0 {
		m.studioPersonaRelatedIndex = 0
	}
	if m.studioPersonaRelatedIndex >= n {
		m.studioPersonaRelatedIndex = n - 1
	}
}

func studioPersonaRows(bundle config.Bundle, known []string) []studioPersonaRow {
	return studioprojection.PersonaRows(bundle, known)
}

// PersonaIndexFor returns the roster index of slug, or 0.
func PersonaIndexFor(bundle config.Bundle, slug string) int {
	return studioprojection.PersonaIndexFor(bundle, slug)
}

func personaSchemaLabel(persona config.Persona) string {
	if persona.SchemaVersion <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", persona.SchemaVersion)
}

func personaSourceLabel(persona config.Persona) string {
	if persona.Slug == "" {
		return "-"
	}
	return "personas/" + persona.Slug + ".md"
}

func studioPersonaRosterLabel(bundle config.Bundle) string {
	key := strings.ToLower(strings.TrimSpace(bundle.Kit.Key))
	if key == "" || key == "omakase" {
		return "naruto"
	}
	return screenkit.Sanitize(key)
}

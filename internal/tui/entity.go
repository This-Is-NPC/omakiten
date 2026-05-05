package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

const (
	entityListWidth = 28
	// Token-count thresholds for the colored token badge on entity cards.
	// Above tokenBadgeRedAt → red; above tokenBadgeYellowAt → yellow; else green.
	tokenBadgeYellowAt = 50
	tokenBadgeRedAt    = 200
)

type entityKind int

const (
	entityKindLaw entityKind = iota
	entityKindPersona
	entityKindSkill
)

type entityScreenMode int

const (
	entityScreenClosed entityScreenMode = iota
	entityScreenView
	entityScreenSkillPicker
)

// entityForm carries the per-screen state. For Phase 1 it only holds the
// detail-view target and (for personas) the skill picker selection set.
type entityForm struct {
	kind         entityKind
	mode         entityScreenMode
	slug         string
	pickerCursor int
	pickerChecks map[string]bool
}

func (k entityKind) String() string {
	switch k {
	case entityKindLaw:
		return "Law"
	case entityKindPersona:
		return "Persona"
	case entityKindSkill:
		return "Skill"
	default:
		return ""
	}
}

func (k entityKind) plural() string {
	return k.String() + "s"
}

func entityKinds() []entityKind {
	return []entityKind{entityKindLaw, entityKindPersona, entityKindSkill}
}

func (m Model) entityCount(kind entityKind) int {
	switch kind {
	case entityKindLaw:
		return len(m.laws)
	case entityKindPersona:
		return len(m.personas)
	case entityKindSkill:
		return len(m.skills)
	}
	return 0
}

func (m Model) selectedEntityIndex(kind entityKind) int {
	if m.entityCursors == nil {
		return 0
	}
	cursor := m.entityCursors[kind]
	count := m.entityCount(kind)
	if count == 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= count {
		return count - 1
	}
	return cursor
}

func (m *Model) clampEntityCursor() {
	if m.entityCursors == nil {
		m.entityCursors = map[entityKind]int{}
	}
	for _, kind := range entityKinds() {
		count := m.entityCount(kind)
		cursor := m.entityCursors[kind]
		switch {
		case count == 0:
			m.entityCursors[kind] = 0
		case cursor < 0:
			m.entityCursors[kind] = 0
		case cursor >= count:
			m.entityCursors[kind] = count - 1
		}
	}
}

func (m Model) renderEntityCell(kind entityKind) string {
	focused := m.entityKind == kind
	count := m.entityCount(kind)
	cursor := m.selectedEntityIndex(kind)

	headerStyle := m.styles.hintAccent
	if !focused {
		headerStyle = m.styles.muted
	}
	headerText := fmt.Sprintf("// %s · %d", strings.ToUpper(kind.plural()), count)

	lines := []string{
		headerStyle.Render(headerText),
		m.styles.separator.Render(strings.Repeat("─", entityListWidth)),
	}

	if count == 0 {
		lines = append(lines, m.styles.empty.Render("empty"))
	} else {
		for index := 0; index < count; index++ {
			lines = append(lines, m.renderEntityCard(kind, index, focused && index == cursor))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderEntityCard(kind entityKind, index int, selected bool) string {
	label := m.entityCardLabel(kind, index)
	wrapped := wrapWords(label, cardContentWidth, cardContentWidth)

	// Badges line (truncated to fit card width)
	badgeLine := m.renderEntityBadges(kind, index, cardContentWidth)

	lines := make([]string, 0, len(wrapped)+1)
	lines = append(lines, wrapped...)
	if badgeLine != "" {
		lines = append(lines, badgeLine)
	}

	style := m.styles.entityCard
	if selected {
		style = m.styles.entityCardSelected
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) renderEntityBadges(kind entityKind, index int, maxWidth int) string {
	switch kind {
	case entityKindLaw:
		return wrapBadges(m.renderLawBadges(index), maxWidth)
	case entityKindPersona:
		return wrapBadges(m.renderPersonaBadges(index), maxWidth)
	case entityKindSkill:
		return wrapBadges(m.renderSkillBadges(index), maxWidth)
	}
	return ""
}

// wrapBadges joins badges with single-space separators, breaking onto a new
// line whenever the next badge would overflow maxWidth. Every badge is kept;
// no truncation. A badge wider than maxWidth on its own occupies its own line.
func wrapBadges(badges []string, maxWidth int) string {
	if len(badges) == 0 {
		return ""
	}
	var lines []string
	var current []string
	currentWidth := 0
	for _, badge := range badges {
		w := lipgloss.Width(badge)
		sep := 0
		if len(current) > 0 {
			sep = 1
		}
		if len(current) > 0 && currentWidth+sep+w > maxWidth {
			lines = append(lines, strings.Join(current, " "))
			current = []string{badge}
			currentWidth = w
			continue
		}
		current = append(current, badge)
		currentWidth += sep + w
	}
	if len(current) > 0 {
		lines = append(lines, strings.Join(current, " "))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderLawBadges(index int) []string {
	law := m.laws[index]
	var badges []string

	// Severity badge
	severity := domain.LawSeverity(law.Severity)
	var severityBadge string
	switch severity {
	case domain.LawSeverityError:
		severityBadge = m.styles.badgeHigh.Render("ERROR")
	case domain.LawSeverityWarning:
		severityBadge = m.styles.badgeBlocker.Render("WARNING")
	default:
		severityBadge = m.styles.badgeInfo.Render("INFO")
	}
	badges = append(badges, severityBadge)

	// Scope badge
	scope := "GLOBAL"
	switch law.Scope {
	case domain.LawScopeProject:
		scope = "PROJECT"
	case domain.LawScopePersona:
		scope = "PERSONA"
	}
	badges = append(badges, m.styles.badgeScope.Render(scope))

	// Token count: matches computeMetrics (key + body) so the per-entity weight
	// matches the totals shown in the Token budget panel.
	tokens := m.counter.Count(law.Key + " " + law.Body)
	badges = append(badges, m.tokenBadge(tokens))

	if strings.TrimSpace(law.Warning) != "" {
		badges = append(badges, m.styles.badgeFix.Render("FIX"))
	}

	return badges
}

func (m Model) renderPersonaBadges(index int) []string {
	persona := m.personas[index]
	// Token count: matches computeMetrics — only the description counts toward
	// the budget. Body is not bundled into context for personas.
	tokens := m.counter.Count(persona.Description)
	badges := []string{m.tokenBadge(tokens)}
	if strings.TrimSpace(persona.Warning) != "" {
		badges = append(badges, m.styles.badgeFix.Render("FIX"))
	}
	return badges
}

func (m Model) renderSkillBadges(index int) []string {
	skill := m.skills[index]
	// Skills are not part of the computeMetrics total — their bodies attach to
	// personas at injection time. The badge is informational so users can see
	// how heavy a skill body is before wiring it.
	tokens := m.counter.Count(skill.Body)
	badges := []string{m.tokenBadge(tokens)}
	if strings.TrimSpace(skill.Warning) != "" {
		badges = append(badges, m.styles.badgeFix.Render("FIX"))
	}
	return badges
}

func (m Model) tokenBadge(tokens int) string {
	label := fmt.Sprintf("TOKENS:%d", tokens)
	switch {
	case tokens > tokenBadgeRedAt:
		return m.styles.badgeTokenRed.Render(label)
	case tokens > tokenBadgeYellowAt:
		return m.styles.badgeTokenYellow.Render(label)
	default:
		return m.styles.badgeTokenGreen.Render(label)
	}
}

func (m Model) entityCardLabel(kind entityKind, index int) string {
	switch kind {
	case entityKindLaw:
		return m.laws[index].Key
	case entityKindPersona:
		return m.personas[index].Key
	case entityKindSkill:
		return m.skills[index].Key
	}
	return ""
}

func (m Model) severityStyle(severity domain.LawSeverity) lipgloss.Style {
	switch severity {
	case domain.LawSeverityError:
		return m.styles.error
	case domain.LawSeverityWarning:
		return m.styles.warning
	case domain.LawSeverityInfo:
		return m.styles.info
	}
	return m.styles.muted
}

// editorFinishedMsg is emitted after $EDITOR exits via tea.ExecProcess. The
// model handler re-imports the bundle so SQLite reflects the user's edits.
type editorFinishedMsg struct {
	err error
}

func (m *Model) handleConfigKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if m.deletePending {
			m.clearDeletePrompt("Delete cancelled")
		}
	case "left", "h":
		m.clearDeletePrompt("")
		m.cycleEntityKind(-1)
	case "right", "l":
		m.clearDeletePrompt("")
		m.cycleEntityKind(1)
	case "up", "k":
		m.clearDeletePrompt("")
		m.moveEntityCursor(-1)
	case "down", "j":
		m.clearDeletePrompt("")
		m.moveEntityCursor(1)
	case "enter":
		m.clearDeletePrompt("")
		m.openSelectedEntityView()
	case "n":
		m.clearDeletePrompt("")
		return m.openEntityCreate(m.entityKind)
	case "e":
		m.clearDeletePrompt("")
		return m.openSelectedEntityEdit()
	case "d":
		m.requestSelectedEntityDelete()
	case "p":
		m.clearDeletePrompt("")
		if m.entityKind == entityKindPersona {
			m.openPersonaPickerForSelected()
		}
	}
	return nil
}

func (m *Model) cycleEntityKind(delta int) {
	kinds := entityKinds()
	current := 0
	for index, kind := range kinds {
		if kind == m.entityKind {
			current = index
			break
		}
	}
	current = (current + delta + len(kinds)) % len(kinds)
	m.entityKind = kinds[current]
}

func (m *Model) moveEntityCursor(delta int) {
	if m.entityCursors == nil {
		m.entityCursors = map[entityKind]int{}
	}
	count := m.entityCount(m.entityKind)
	if count == 0 {
		m.entityCursors[m.entityKind] = 0
		return
	}
	cursor := m.entityCursors[m.entityKind] + delta
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= count {
		cursor = count - 1
	}
	m.entityCursors[m.entityKind] = cursor
}

func (m *Model) openSelectedEntityView() {
	if m.entityCount(m.entityKind) == 0 {
		m.status = "Nothing to open"
		return
	}
	cursor := m.selectedEntityIndex(m.entityKind)
	slug := m.entitySlugAt(m.entityKind, cursor)
	if slug == "" {
		return
	}
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{kind: m.entityKind, mode: entityScreenView, slug: slug}
	m.status = ""
	m.entityViewScroll = 0
}

// openEntityCreate scaffolds a new entity file and runs $EDITOR against it.
// The returned tea.Cmd suspends the TUI for the editor process and re-imports
// on return.
func (m *Model) openEntityCreate(kind entityKind) tea.Cmd {
	if m.repos.Editor == nil {
		m.status = "Editor not available"
		return nil
	}
	name := nextScaffoldName(kind, m.snapshot())
	path, err := scaffoldEntity(m.ctx, kind, m.repos, name)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	}
	return runExternalEditor(path)
}

// snapshot returns a value-receiver copy of m suitable for read-only helpers.
func (m *Model) snapshot() Model { return *m }

// bundleWarningIndex returns the first source-warning message keyed by slug.
// Mirrors app.warningIndex so the TUI's enrich pipeline can surface the same
// non-fatal issues the CLI shows in `okt skill list` etc.
func bundleWarningIndex(warnings []config.SourceWarning) map[string]string {
	out := map[string]string{}
	for _, w := range warnings {
		if w.Slug == "" {
			continue
		}
		if _, exists := out[w.Slug]; exists {
			continue
		}
		out[w.Slug] = w.Message
	}
	return out
}

// enrichSkillsFromBundle merges the on-disk frontmatter + body + source path
// into the identity-level skill records returned by the SQLite store.
func enrichSkillsFromBundle(skills []domain.Skill, bundle config.Bundle) []domain.Skill {
	bySlug := map[string]config.Skill{}
	for _, skill := range bundle.Skills {
		bySlug[skill.Slug] = skill
	}
	warnings := bundleWarningIndex(bundle.Warnings)
	for index, skill := range skills {
		if file, ok := bySlug[skill.Key]; ok {
			skills[index].Description = file.Description
			skills[index].Body = file.Body
			skills[index].SourcePath = file.SourcePath
			if file.Name != "" {
				skills[index].Name = file.Name
			}
		}
		if w, ok := warnings[skill.Key]; ok {
			skills[index].Warning = w
		}
	}
	return skills
}

func enrichLawsFromBundle(laws []domain.Law, bundle config.Bundle) []domain.Law {
	bySlug := map[string]config.Law{}
	for _, law := range bundle.Laws {
		bySlug[law.Slug] = law
	}
	warnings := bundleWarningIndex(bundle.Warnings)
	for index, law := range laws {
		if file, ok := bySlug[law.Key]; ok {
			laws[index].Body = file.Body
			laws[index].Severity = file.Severity
			laws[index].SourcePath = file.SourcePath
			laws[index].Scope = domain.LawScope(file.Scope)
			laws[index].ProjectKey = file.ProjectSlug
			laws[index].PersonaKey = file.PersonaSlug
			if file.Name != "" {
				laws[index].Name = file.Name
			}
		}
		if w, ok := warnings[law.Key]; ok {
			laws[index].Warning = w
		}
	}
	return laws
}

func enrichPersonasFromBundle(personas []domain.Persona, bundle config.Bundle) []domain.Persona {
	bySlug := map[string]config.Persona{}
	for _, persona := range bundle.Personas {
		bySlug[persona.Slug] = persona
	}
	warnings := bundleWarningIndex(bundle.Warnings)
	for index, persona := range personas {
		if file, ok := bySlug[persona.Key]; ok {
			personas[index].Description = file.Description
			personas[index].Body = file.Body
			personas[index].SourcePath = file.SourcePath
			personas[index].LawKeys = append([]string(nil), file.Laws...)
			if file.Name != "" {
				personas[index].Name = file.Name
			}
		}
		if w, ok := warnings[persona.Key]; ok {
			personas[index].Warning = w
		}
	}
	return personas
}

func (m *Model) openSelectedEntityEdit() tea.Cmd {
	if m.entityCount(m.entityKind) == 0 {
		m.status = "Nothing to edit"
		return nil
	}
	cursor := m.selectedEntityIndex(m.entityKind)
	slug := m.entitySlugAt(m.entityKind, cursor)
	if slug == "" {
		return nil
	}
	return m.openEntityEditor(m.entityKind, slug)
}

func (m *Model) openEntityEditor(kind entityKind, slug string) tea.Cmd {
	path := m.entitySourcePath(kind, slug)
	if path == "" {
		m.status = "Source path missing"
		return nil
	}
	return runExternalEditor(path)
}

// runExternalEditor builds a tea.ExecProcess command that invokes $EDITOR on
// path and reports completion via editorFinishedMsg.
func runExternalEditor(path string) tea.Cmd {
	editor := app.ResolveEditor()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return func() tea.Msg { return editorFinishedMsg{err: fmt.Errorf("editor not configured")} }
	}
	args := append(parts[1:], path)
	cmd := exec.Command(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m *Model) entitySlugAt(kind entityKind, index int) string {
	switch kind {
	case entityKindLaw:
		if index < 0 || index >= len(m.laws) {
			return ""
		}
		return m.laws[index].Key
	case entityKindPersona:
		if index < 0 || index >= len(m.personas) {
			return ""
		}
		return m.personas[index].Key
	case entityKindSkill:
		if index < 0 || index >= len(m.skills) {
			return ""
		}
		return m.skills[index].Key
	}
	return ""
}

func (m Model) entitySourcePath(kind entityKind, slug string) string {
	switch kind {
	case entityKindLaw:
		if law, ok := m.findLawBySlug(slug); ok {
			return law.SourcePath
		}
	case entityKindPersona:
		if persona, ok := m.findPersonaBySlug(slug); ok {
			return persona.SourcePath
		}
	case entityKindSkill:
		if skill, ok := m.findSkillBySlug(slug); ok {
			return skill.SourcePath
		}
	}
	return ""
}

// updateEntityScreen handles input while a detail view or persona picker is
// open. Returns whether handling consumed the message and any cmd to dispatch.
func (m Model) updateEntityScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.entityForm.mode == entityScreenSkillPicker {
		return m.updatePersonaPicker(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.deletePending {
			m.clearDeletePrompt("Delete cancelled")
			return m, nil
		}
		m.closeEntityScreen("")
	case "e":
		m.clearDeletePrompt("")
		return m, m.openEntityEditor(m.entityForm.kind, m.entityForm.slug)
	case "d":
		m.requestEntityDelete(m.entityForm.kind, m.entityForm.slug)
	case "p":
		m.clearDeletePrompt("")
		if m.entityForm.kind == entityKindPersona {
			m.openPersonaPicker(m.entityForm.slug)
		}
	case "r":
		m.clearDeletePrompt("")
		if err := m.refresh(); err != nil {
			m.status = err.Error()
		} else {
			m.status = "Refreshed"
		}
	case "j", "down":
		m.entityViewScroll++
	case "k", "up":
		if m.entityViewScroll > 0 {
			m.entityViewScroll--
		}
	case "pgdown", "ctrl+d":
		m.entityViewScroll += taskViewPageStep(m.entityViewportHeight())
	case "pgup", "ctrl+u":
		m.entityViewScroll -= taskViewPageStep(m.entityViewportHeight())
		if m.entityViewScroll < 0 {
			m.entityViewScroll = 0
		}
	case "home", "g":
		m.entityViewScroll = 0
	case "end", "G":
		m.entityViewScroll = 1 << 20
	}
	return m, nil
}

// handleEditorFinished is the post-editor callback. Re-imports the bundle and
// refreshes the model state so the freshly written file is reflected.
func (m *Model) handleEditorFinished(msg editorFinishedMsg) {
	if msg.err != nil {
		m.status = "Editor: " + msg.err.Error()
		return
	}
	if m.repos.Editor != nil {
		if _, err := m.repos.Editor.Apply(m.ctx, nil); err != nil {
			m.status = err.Error()
			return
		}
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.status = "Saved"
}

func (m *Model) requestSelectedEntityDelete() {
	if m.entityCount(m.entityKind) == 0 {
		m.status = "Nothing to delete"
		return
	}
	cursor := m.selectedEntityIndex(m.entityKind)
	slug := m.entitySlugAt(m.entityKind, cursor)
	if slug == "" {
		return
	}
	m.requestEntityDelete(m.entityKind, slug)
}

func (m *Model) requestEntityDelete(kind entityKind, slug string) {
	if slug == "" {
		return
	}
	if m.deletePending && m.deleteKind == kind && m.deleteSlug == slug {
		m.deleteEntity(kind, slug)
		return
	}
	m.deletePending = true
	m.deleteKind = kind
	m.deleteSlug = slug
	m.status = fmt.Sprintf("Confirm delete %s %q. Press d again to remove it; esc cancels.", strings.ToLower(kind.String()), slug)
}

func (m *Model) deleteEntity(kind entityKind, slug string) {
	if m.repos.Editor == nil {
		m.status = "Editor not available"
		return
	}
	var err error
	switch kind {
	case entityKindLaw:
		err = app.NewLawService(m.repos.Config, m.repos.Editor).Remove(m.ctx, slug)
	case entityKindSkill:
		err = app.NewSkillService(m.repos.Config, m.repos.Editor).Remove(m.ctx, slug)
	case entityKindPersona:
		err = app.NewPersonaService(m.repos.Config, m.repos.Editor).Remove(m.ctx, slug)
	}
	if err != nil {
		m.status = err.Error()
		return
	}
	m.clearDeletePrompt("")
	if refreshErr := m.refresh(); refreshErr != nil {
		m.status = refreshErr.Error()
		return
	}
	if m.entityScreen == entityScreenView && m.entityForm.slug == slug {
		m.closeEntityScreen("Deleted")
		return
	}
	m.status = "Deleted"
}

func (m *Model) closeEntityScreen(status string) {
	m.clearDeletePrompt("")
	m.entityScreen = entityScreenClosed
	m.entityForm = entityForm{}
	m.status = status
	m.entityViewScroll = 0
}

func (m *Model) clearDeletePrompt(status string) {
	m.deletePending = false
	m.deleteKind = entityKindLaw
	m.deleteSlug = ""
	if status != "" {
		m.status = status
	}
}

func (m Model) renderEntityScreen() string {
	switch m.entityForm.mode {
	case entityScreenView:
		return m.renderEntityView()
	case entityScreenSkillPicker:
		return m.renderPersonaPicker()
	}
	return ""
}

func (m Model) renderEntityView() string {
	const (
		entityDetailLabelWidth = 14
		entityDetailMinValue   = 20
		entityDetailMaxValue   = 140
	)
	available := m.availableWidth() - 4
	minTotal := entityDetailLabelWidth + entityDetailMinValue + 3
	maxTotal := entityDetailLabelWidth + entityDetailMaxValue + 3
	totalWidth := clampInt(available, minTotal, maxTotal)
	valueWidth := totalWidth - entityDetailLabelWidth - 3

	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}

	header := m.styles.kicker(fmt.Sprintf("%s · %s", m.entityForm.kind.String(), m.entityForm.slug))

	var dataRows [][]string
	var body string
	var extraSpannedRows [][]string

	switch m.entityForm.kind {
	case entityKindLaw:
		law, ok := m.findLawBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Law not found"), 2)
		}
		badge := m.severityStyle(domain.LawSeverity(law.Severity)).Render(law.Severity)
		dataRows = [][]string{
			{labelCell("Slug"), law.Key},
			{labelCell("Severity"), badge},
			{labelCell("Source"), law.SourcePath},
		}
		body = law.Body
	case entityKindSkill:
		skill, ok := m.findSkillBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Skill not found"), 2)
		}
		dataRows = [][]string{
			{labelCell("Slug"), skill.Key},
			{labelCell("Name"), skill.Name},
			{labelCell("Description"), skill.Description},
			{labelCell("Source"), skill.SourcePath},
		}
		body = skill.Body
	case entityKindPersona:
		persona, ok := m.findPersonaBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Persona not found"), 2)
		}
		skills := strings.Join(persona.SkillKeys, ", ")
		if skills == "" {
			skills = m.styles.hint.Render("none")
		}
		dataRows = [][]string{
			{labelCell("Slug"), persona.Key},
			{labelCell("Name"), persona.Name},
			{labelCell("Description"), persona.Description},
			{labelCell("Skills"), skills},
			{labelCell("Source"), persona.SourcePath},
		}
		body = persona.Body
		extraSpannedRows = [][]string{{m.styles.hint.Render("p: open skill picker")}}
	}

	bodyText := strings.TrimRight(body, "\n")
	if strings.TrimSpace(bodyText) == "" {
		bodyText = m.styles.hint.Render("Empty body")
	}

	rows := [][]string{{header}}
	rows = append(rows, dataRows...)
	rows = append(rows,
		[]string{m.styles.kicker("Body")},
		[]string{bodyText},
	)
	rows = append(rows, extraSpannedRows...)

	table := renderGridTable(rows, []int{entityDetailLabelWidth, valueWidth}, m.styles.border)
	return m.applyEntityViewScroll(table)
}

// entityViewportHeight is the line budget for the entity detail view content
// between header and footer. Returns 0 when the height is unknown or too small
// to scroll usefully — callers should render everything and let the terminal
// scroll natively in that case.
func (m Model) entityViewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 5 // header(2) + leading blank(1) + footer(2)
	if m.status != "" {
		chrome++
	}
	h := m.height - chrome
	if h < 8 {
		return 0
	}
	return h
}

// applyEntityViewScroll slices the rendered grid to the available viewport
// based on m.entityViewScroll. Operates on the post-render line list (no
// height heuristics) so very tall bodies behave deterministically.
func (m Model) applyEntityViewScroll(content string) string {
	viewport := m.entityViewportHeight()
	lines := strings.Split(content, "\n")
	if viewport <= 0 || len(lines) <= viewport {
		return "\n" + indentBlock(content, 2)
	}

	visibleHeight := viewport - 1
	maxOffset := len(lines) - visibleHeight
	offset := m.entityViewScroll
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	visible := lines[offset : offset+visibleHeight]
	above := offset
	below := len(lines) - (offset + visibleHeight)
	if below < 0 {
		below = 0
	}
	hint := m.styles.hint.Render(fmt.Sprintf("▲ %d above · ▼ %d below  · j/k pgup/pgdn g/G", above, below))
	return "\n" + indentBlock(strings.Join(visible, "\n")+"\n"+hint, 2)
}

// openPersonaPicker initializes the multi-select picker for the persona at
// slug. It pre-checks every skill the persona currently references and lists
// every loaded skill as a row. Submission writes only the wiring entry.
func (m *Model) openPersonaPicker(slug string) {
	persona, ok := m.findPersonaBySlug(slug)
	if !ok {
		m.status = "Persona not found"
		return
	}
	checks := map[string]bool{}
	for _, key := range persona.SkillKeys {
		checks[key] = true
	}
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{
		kind:         entityKindPersona,
		mode:         entityScreenSkillPicker,
		slug:         slug,
		pickerCursor: 0,
		pickerChecks: checks,
	}
	m.status = "Skill picker"
	m.pickerScroll = 0
}

func (m *Model) openPersonaPickerForSelected() {
	if m.entityCount(entityKindPersona) == 0 {
		m.status = "No persona selected"
		return
	}
	cursor := m.selectedEntityIndex(entityKindPersona)
	slug := m.entitySlugAt(entityKindPersona, cursor)
	if slug == "" {
		return
	}
	m.openPersonaPicker(slug)
}

func (m Model) updatePersonaPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rowCount := len(m.skills) + 1 // +1 for "create new skill" affordance
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.openSelectedEntityViewForSlug(entityKindPersona, m.entityForm.slug)
	case "up", "k":
		if m.entityForm.pickerCursor > 0 {
			m.entityForm.pickerCursor--
			m.syncPickerScroll(rowCount)
		}
	case "down", "j":
		if m.entityForm.pickerCursor < rowCount-1 {
			m.entityForm.pickerCursor++
			m.syncPickerScroll(rowCount)
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.pickerViewportRows())
		m.entityForm.pickerCursor -= step
		if m.entityForm.pickerCursor < 0 {
			m.entityForm.pickerCursor = 0
		}
		m.syncPickerScroll(rowCount)
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.pickerViewportRows())
		m.entityForm.pickerCursor += step
		if m.entityForm.pickerCursor > rowCount-1 {
			m.entityForm.pickerCursor = rowCount - 1
		}
		if m.entityForm.pickerCursor < 0 {
			m.entityForm.pickerCursor = 0
		}
		m.syncPickerScroll(rowCount)
	case "home", "g":
		m.entityForm.pickerCursor = 0
		m.syncPickerScroll(rowCount)
	case "end", "G":
		m.entityForm.pickerCursor = rowCount - 1
		if m.entityForm.pickerCursor < 0 {
			m.entityForm.pickerCursor = 0
		}
		m.syncPickerScroll(rowCount)
	case " ", "space":
		if m.entityForm.pickerCursor < len(m.skills) {
			slug := m.skills[m.entityForm.pickerCursor].Key
			if m.entityForm.pickerChecks == nil {
				m.entityForm.pickerChecks = map[string]bool{}
			}
			m.entityForm.pickerChecks[slug] = !m.entityForm.pickerChecks[slug]
		}
	case "enter":
		// "enter" on the last row triggers the "+ create new skill" flow.
		if m.entityForm.pickerCursor == len(m.skills) {
			return m, m.scaffoldNewSkillFromPicker()
		}
	case "ctrl+s":
		m.savePersonaPicker()
	}
	return m, nil
}

// syncPickerScroll keeps m.pickerScroll aligned so the cursor row stays
// visible. Same cursor-following pattern as syncTableScroll/syncLogsScroll —
// each picker row is exactly 1 line so no height heuristic is needed.
func (m *Model) syncPickerScroll(rowCount int) {
	viewport := m.pickerViewportRows()
	if viewport <= 0 {
		return
	}
	if m.entityForm.pickerCursor < m.pickerScroll {
		m.pickerScroll = m.entityForm.pickerCursor
	}
	if m.entityForm.pickerCursor >= m.pickerScroll+viewport {
		m.pickerScroll = m.entityForm.pickerCursor - viewport + 1
	}
	if m.pickerScroll < 0 {
		m.pickerScroll = 0
	}
	if m.pickerScroll > rowCount-1 {
		m.pickerScroll = rowCount - 1
	}
	if m.pickerScroll < 0 {
		m.pickerScroll = 0
	}
}

// pickerViewportRows returns how many picker rows fit between the screen
// chrome and the panel's internal header rows.
func (m Model) pickerViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 2 entity-mode header + 1 leading blank + 2 footer + 2 panel borders
	// + 4 panel header rows (kicker/hint/blank/separator) = 11.
	chrome := 11
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

// savePersonaPicker writes only the persona wiring (skills slugs) without
// touching the persona body file. Selection is the set of checked rows.
func (m *Model) savePersonaPicker() {
	if m.repos.Editor == nil {
		m.status = "Editor not available"
		return
	}
	slugs := make([]string, 0, len(m.entityForm.pickerChecks))
	for _, skill := range m.skills {
		if m.entityForm.pickerChecks[skill.Key] {
			slugs = append(slugs, skill.Key)
		}
	}
	service := app.NewPersonaService(m.repos.Config, m.repos.Editor)
	keys := slugs
	if _, err := service.Edit(m.ctx, m.entityForm.slug, domain.PersonaUpdate{SkillKeys: &keys}); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.openSelectedEntityViewForSlug(entityKindPersona, m.entityForm.slug)
	m.status = "Saved"
}

// scaffoldNewSkillFromPicker creates a placeholder skill, opens $EDITOR
// against it, and pre-checks it on the picker once the editor returns.
func (m *Model) scaffoldNewSkillFromPicker() tea.Cmd {
	name := nextScaffoldName(entityKindSkill, m.snapshot())
	path, err := scaffoldEntity(m.ctx, entityKindSkill, m.repos, name)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	// Pre-check the new skill so it is selected on the picker after editor exits.
	if m.entityForm.pickerChecks == nil {
		m.entityForm.pickerChecks = map[string]bool{}
	}
	// The slug derives deterministically from the scaffold name.
	slug := slugFromName(name)
	m.entityForm.pickerChecks[slug] = true
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	}
	return runExternalEditor(path)
}

func (m *Model) openSelectedEntityViewForSlug(kind entityKind, slug string) {
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{kind: kind, mode: entityScreenView, slug: slug}
	m.entityViewScroll = 0
}

func (m Model) renderPersonaPicker() string {
	persona, _ := m.findPersonaBySlug(m.entityForm.slug)
	contentWidth := m.availableWidth() - 4

	skills := append([]domain.Skill(nil), m.skills...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].Key < skills[j].Key })

	dataRows := make([]string, 0, len(skills)+1)
	for index, skill := range skills {
		marker := normalMarker
		check := "[ ]"
		if m.entityForm.pickerChecks[skill.Key] {
			check = "[x]"
		}
		if m.entityForm.pickerCursor == index {
			marker = m.styles.marker.Render(selectionMarker)
		}
		dataRows = append(dataRows, fmt.Sprintf("%s %s %s — %s", marker, check, skill.Key, skill.Name))
	}
	addMarker := normalMarker
	if m.entityForm.pickerCursor == len(skills) {
		addMarker = m.styles.marker.Render(selectionMarker)
	}
	dataRows = append(dataRows, fmt.Sprintf("%s + create new skill (opens $EDITOR)", addMarker))

	lines := []string{
		m.styles.kicker(fmt.Sprintf("Skills for persona · %s", persona.Key)),
		m.styles.hint.Render("up/down: move · space: toggle · enter on '+ create new': new skill · ctrl+s: save · esc: cancel"),
		"",
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	lines = append(lines, m.sliceScrollRows(dataRows, m.pickerScroll, m.pickerViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(lines, "\n")), 2)
}

func (m Model) findLawBySlug(slug string) (domain.Law, bool) {
	for _, law := range m.laws {
		if law.Key == slug {
			return law, true
		}
	}
	return domain.Law{}, false
}

func (m Model) findSkillBySlug(slug string) (domain.Skill, bool) {
	for _, skill := range m.skills {
		if skill.Key == slug {
			return skill, true
		}
	}
	return domain.Skill{}, false
}

func (m Model) findPersonaBySlug(slug string) (domain.Persona, bool) {
	for _, persona := range m.personas {
		if persona.Key == slug {
			return persona, true
		}
	}
	return domain.Persona{}, false
}

// nextScaffoldName picks a unique placeholder name like "New skill 1" so that
// the user can rename it inside $EDITOR. The slug derives from the chosen name.
func nextScaffoldName(kind entityKind, m Model) string {
	prefix := "New " + strings.ToLower(kind.String())
	existing := map[string]struct{}{}
	switch kind {
	case entityKindLaw:
		for _, law := range m.laws {
			existing[law.Key] = struct{}{}
		}
	case entityKindSkill:
		for _, skill := range m.skills {
			existing[skill.Key] = struct{}{}
		}
	case entityKindPersona:
		for _, persona := range m.personas {
			existing[persona.Key] = struct{}{}
		}
	}
	for n := 1; n < 1000; n++ {
		candidate := fmt.Sprintf("%s %d", prefix, n)
		slug := slugFromName(candidate)
		if _, taken := existing[slug]; !taken {
			return candidate
		}
	}
	return prefix
}

// slugFromName mirrors config.Slugify without forcing a config import inside
// the TUI hot path.
func slugFromName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// scaffoldEntity calls into the appropriate service to create a placeholder
// entity file and returns its absolute path so the TUI can hand it to $EDITOR.
func scaffoldEntity(ctx context.Context, kind entityKind, repos Repositories, name string) (string, error) {
	switch kind {
	case entityKindSkill:
		service := app.NewSkillService(repos.Config, repos.Editor)
		skill, err := service.Add(ctx, domain.SkillInput{Name: name})
		if err != nil {
			return "", err
		}
		return skill.SourcePath, nil
	case entityKindLaw:
		service := app.NewLawService(repos.Config, repos.Editor)
		law, err := service.Add(ctx, domain.LawInput{
			Key:      slugFromName(name),
			Name:     name,
			Severity: domain.LawSeverityError,
			Body:     "TODO: write the law body.",
		})
		if err != nil {
			return "", err
		}
		return law.SourcePath, nil
	case entityKindPersona:
		service := app.NewPersonaService(repos.Config, repos.Editor)
		persona, err := service.Add(ctx, domain.PersonaInput{Name: name})
		if err != nil {
			return "", err
		}
		return persona.SourcePath, nil
	}
	return "", fmt.Errorf("unknown entity kind")
}

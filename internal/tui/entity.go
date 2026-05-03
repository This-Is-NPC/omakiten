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
	entityListWidth = 30
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
		lines = append(lines, m.styles.empty.Render(centerText("empty", entityListWidth)))
	} else {
		for index := 0; index < count; index++ {
			lines = append(lines, m.renderEntityRow(kind, index, focused && index == cursor))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderEntityRow(kind entityKind, index int, selected bool) string {
	marker := normalMarker
	if selected {
		marker = m.styles.marker.Render(selectionMarker)
	}
	indicator := m.entityIndicator(kind, index).Render("●")
	label := m.entityRowLabel(kind, index)
	prefix := fmt.Sprintf("%s %s ", marker, indicator)
	budget := entityListWidth - lipgloss.Width(prefix)
	return m.styles.card.Render(prefix + truncateText(label, budget))
}

func (m Model) entityRowLabel(kind entityKind, index int) string {
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

func (m Model) entityIndicator(kind entityKind, index int) lipgloss.Style {
	if kind == entityKindLaw {
		return m.severityStyle(domain.LawSeverity(m.laws[index].Severity))
	}
	return m.styles.success
}

func (m Model) severityStyle(severity domain.LawSeverity) lipgloss.Style {
	switch severity {
	case domain.LawSeverityError:
		return m.styles.error
	case domain.LawSeverityWarning:
		return m.styles.warning
	case domain.LawSeverityInfo:
		return m.styles.success
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
	case "left", "h":
		m.cycleEntityKind(-1)
	case "right", "l":
		m.cycleEntityKind(1)
	case "up", "k":
		m.moveEntityCursor(-1)
	case "down", "j":
		m.moveEntityCursor(1)
	case "enter":
		m.openSelectedEntityView()
	case "n":
		return m.openEntityCreate(m.entityKind)
	case "e":
		return m.openSelectedEntityEdit()
	case "d":
		m.deleteSelectedEntity()
	case "p":
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

// enrichSkillsFromBundle merges the on-disk frontmatter + body + source path
// into the identity-level skill records returned by the SQLite store.
func enrichSkillsFromBundle(skills []domain.Skill, bundle config.Bundle) []domain.Skill {
	bySlug := map[string]config.Skill{}
	for _, skill := range bundle.Skills {
		bySlug[skill.Slug] = skill
	}
	for index, skill := range skills {
		if file, ok := bySlug[skill.Key]; ok {
			skills[index].Description = file.Description
			skills[index].Body = file.Body
			skills[index].SourcePath = file.SourcePath
			if file.Name != "" {
				skills[index].Name = file.Name
			}
		}
	}
	return skills
}

func enrichLawsFromBundle(laws []domain.Law, bundle config.Bundle) []domain.Law {
	bySlug := map[string]config.Law{}
	for _, law := range bundle.Laws {
		bySlug[law.Slug] = law
	}
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
	}
	return laws
}

func enrichPersonasFromBundle(personas []domain.Persona, bundle config.Bundle) []domain.Persona {
	bySlug := map[string]config.Persona{}
	for _, persona := range bundle.Personas {
		bySlug[persona.Slug] = persona
	}
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
		m.closeEntityScreen("")
	case "e":
		return m, m.openEntityEditor(m.entityForm.kind, m.entityForm.slug)
	case "d":
		m.deleteEntity(m.entityForm.kind, m.entityForm.slug)
	case "p":
		if m.entityForm.kind == entityKindPersona {
			m.openPersonaPicker(m.entityForm.slug)
		}
	case "r":
		if err := m.refresh(); err != nil {
			m.status = err.Error()
		} else {
			m.status = "Refreshed"
		}
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

func (m *Model) deleteSelectedEntity() {
	if m.entityCount(m.entityKind) == 0 {
		m.status = "Nothing to delete"
		return
	}
	cursor := m.selectedEntityIndex(m.entityKind)
	slug := m.entitySlugAt(m.entityKind, cursor)
	if slug == "" {
		return
	}
	m.deleteEntity(m.entityKind, slug)
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
	m.entityScreen = entityScreenClosed
	m.entityForm = entityForm{}
	m.status = status
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
	lines := []string{
		m.styles.kicker(fmt.Sprintf("%s · %s", m.entityForm.kind.String(), m.entityForm.slug)),
		"",
	}
	switch m.entityForm.kind {
	case entityKindLaw:
		law, ok := m.findLawBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Law not found"), 2)
		}
		badge := m.severityStyle(domain.LawSeverity(law.Severity)).Render(law.Severity)
		lines = append(lines,
			m.styles.metaRow("Slug", law.Key, 14),
			m.styles.metaRow("Severity", badge, 14),
			m.styles.metaRow("Source", law.SourcePath, 14),
			"",
			m.styles.kicker("Body"),
			law.Body,
		)
	case entityKindSkill:
		skill, ok := m.findSkillBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Skill not found"), 2)
		}
		lines = append(lines,
			m.styles.metaRow("Slug", skill.Key, 16),
			m.styles.metaRow("Name", skill.Name, 16),
			m.styles.metaRow("Description", skill.Description, 16),
			m.styles.metaRow("Source", skill.SourcePath, 16),
			"",
			m.styles.kicker("Body"),
			skill.Body,
		)
	case entityKindPersona:
		persona, ok := m.findPersonaBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Persona not found"), 2)
		}
		skills := strings.Join(persona.SkillKeys, ", ")
		if skills == "" {
			skills = m.styles.hint.Render("none")
		}
		lines = append(lines,
			m.styles.metaRow("Slug", persona.Key, 16),
			m.styles.metaRow("Name", persona.Name, 16),
			m.styles.metaRow("Description", persona.Description, 16),
			m.styles.metaRow("Skills", skills, 16),
			m.styles.metaRow("Source", persona.SourcePath, 16),
			"",
			m.styles.kicker("Body"),
			persona.Body,
			"",
			m.styles.hint.Render("p: open skill picker"),
		)
	}
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(lines, "\n")), 2)
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
		}
	case "down", "j":
		if m.entityForm.pickerCursor < rowCount-1 {
			m.entityForm.pickerCursor++
		}
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
}

func (m Model) renderPersonaPicker() string {
	persona, _ := m.findPersonaBySlug(m.entityForm.slug)
	lines := []string{
		m.styles.kicker(fmt.Sprintf("Skills for persona · %s", persona.Key)),
		m.styles.hint.Render("up/down: move · space: toggle · enter on '+ create new': new skill · ctrl+s: save · esc: cancel"),
		"",
	}
	skills := append([]domain.Skill(nil), m.skills...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].Key < skills[j].Key })
	for index, skill := range skills {
		marker := " "
		check := "[ ]"
		if m.entityForm.pickerChecks[skill.Key] {
			check = "[x]"
		}
		if m.entityForm.pickerCursor == index {
			marker = ">"
		}
		row := fmt.Sprintf("%s %s %s — %s", marker, check, skill.Key, skill.Name)
		lines = append(lines, row)
	}
	addMarker := " "
	if m.entityForm.pickerCursor == len(skills) {
		addMarker = ">"
	}
	lines = append(lines, fmt.Sprintf("%s + create new skill (opens $EDITOR)", addMarker))
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

package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
)

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
	case "D":
		m.clearDeletePrompt("")
		if m.entityKind == entityKindTag && m.repos.Tags != nil {
			m.deleteOrphanTags()
		}
	case "enter":
		m.clearDeletePrompt("")
		if m.entityKind != entityKindTag {
			m.openSelectedEntityView()
		}
	case "n":
		m.clearDeletePrompt("")
		if m.entityKind == entityKindTemplate {
			m.status = "Templates auto-load — add a .md file to templates/ and refresh"
		} else if m.entityKind != entityKindTag {
			return m.openEntityCreate(m.entityKind)
		}
	case "e":
		m.clearDeletePrompt("")
		if m.entityKind != entityKindTag {
			return m.openSelectedEntityEdit()
		}
	case "d":
		switch m.entityKind {
		case entityKindTag:
			m.requestSelectedTagDelete()
		case entityKindTemplate:
			m.status = "Templates auto-load — remove the .md file from templates/ and refresh"
		default:
			m.requestSelectedEntityDelete()
		}
	case "p":
		m.clearDeletePrompt("")
		if m.entityKind == entityKindPersona {
			m.openPersonaPickerForSelected()
		}
	case "t":
		m.clearDeletePrompt("")
		m.openThemePicker()
	case "c":
		m.clearDeletePrompt("")
		m.openConfigPicker()
	case "a":
		m.clearDeletePrompt("")
		if m.entityKind == entityKindTemplate {
			m.openTemplateDefaultPickerForSelected()
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
	m.syncFocusedEntityScroll()
	m.syncEntityKindScroll()
}

func (m *Model) moveEntityCursor(delta int) {
	if m.entityCursors == nil {
		m.entityCursors = map[entityKind]int{}
	}
	count := m.entityCount(m.entityKind)
	if count == 0 {
		m.entityCursors[m.entityKind] = 0
		m.syncFocusedEntityScroll()
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
	m.syncFocusedEntityScroll()
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
	m.entityView = detailscreen.New(0)
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
			skills[index].IsCustom = file.IsCustom
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
			laws[index].IsCustom = file.IsCustom
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
			personas[index].IsCustom = file.IsCustom
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
	case entityKindTemplate:
		if index < 0 || index >= len(m.templates) {
			return ""
		}
		return m.templates[index].Slug
	case entityKindTag:
		if index < 0 || index >= len(m.tags) {
			return ""
		}
		return m.tags[index].Name
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
	case entityKindTemplate:
		if template, ok := m.findTemplateBySlug(slug); ok {
			return template.SourcePath
		}
	}
	return ""
}

// updateEntityScreen handles input while a detail view or persona picker is
// open. Returns whether handling consumed the message and any cmd to dispatch.

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

func (m *Model) deleteOrphanTags() {
	if m.repos.Tags == nil {
		m.status = "Tag repository not available"
		return
	}
	n, err := m.repos.Tags.DeleteOrphanTags(m.ctx)
	if err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if n == 0 {
		m.status = "No orphan tags found"
	} else {
		m.status = fmt.Sprintf("%d orphan tag(s) deleted", n)
	}
}

func (m *Model) requestSelectedTagDelete() {
	if m.repos.Tags == nil || len(m.tags) == 0 {
		m.status = "Nothing to delete"
		return
	}
	cursor := m.selectedEntityIndex(entityKindTag)
	tag := m.tags[cursor]
	if tag.UsageCount > 0 {
		m.status = fmt.Sprintf("Tag %q is in use (%d references) — cannot delete", tag.Label, tag.UsageCount)
		return
	}
	if m.deletePending && m.deleteKind == entityKindTag && m.deleteSlug == tag.Name {
		m.deleteTagByName(tag.Name)
		return
	}
	m.deletePending = true
	m.deleteKind = entityKindTag
	m.deleteSlug = tag.Name
	m.status = fmt.Sprintf("Confirm delete tag %q. Press d again to remove; esc cancels.", tag.Label)
}

func (m *Model) deleteTagByName(name string) {
	if m.repos.Tags == nil {
		m.status = "Tag repository not available"
		return
	}
	n, err := m.repos.Tags.DeleteOrphanTags(m.ctx)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.clearDeletePrompt("")
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if n > 0 {
		m.status = fmt.Sprintf("Tag %q deleted", name)
	}
}

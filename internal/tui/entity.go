package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
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
		err = app.NewLawService(m.repos.Config, m.repos.Editor, m.repos.EntityFiles, m.repos.Slugger).Remove(m.ctx, slug)
	case entityKindSkill:
		err = app.NewSkillService(m.repos.Config, m.repos.Editor, m.repos.EntityFiles, m.repos.Slugger).Remove(m.ctx, slug)
	case entityKindPersona:
		err = app.NewPersonaService(m.repos.Config, m.repos.Editor, m.repos.EntityFiles, m.repos.Slugger).Remove(m.ctx, slug)
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

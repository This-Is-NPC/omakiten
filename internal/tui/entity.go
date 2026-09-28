package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
)

// editorFinishedMsg is emitted after $EDITOR exits via tea.ExecProcess. The
// model handler re-imports the bundle so SQLite reflects the user's edits.
type editorFinishedMsg struct {
	err error
}

func (m *Model) clearDeletePrompt(status string) {
	m.deletePending = false
	m.deleteKind = entityKindLaw
	m.deleteSlug = ""
	m.taskDeletePendingID = 0
	if status != "" {
		m.status = status
	}
}

// openEntityCreate scaffolds a new entity file and runs $EDITOR against it.
// The returned tea.Cmd suspends the TUI for the editor process and re-imports
// on return.
func (m *Model) openEntityCreate(kind entityKind) tea.Cmd {
	if m.repos.Editor == nil {
		m.status = m.t("tui.status.editor_unavailable")
		return nil
	}
	name := nextScaffoldName(kind, m.snapshot())
	path, err := m.scaffoldEntity(m.ctx, kind, m.repos, name)
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

func (m *Model) openEntityEditor(kind entityKind, slug string) tea.Cmd {
	path := m.entitySourcePath(kind, slug)
	if path == "" {
		m.status = m.t("tui.status.source_path_missing")
		return nil
	}
	return runExternalEditor(path)
}

// runExternalEditor builds a tea.ExecProcess command that invokes $EDITOR on
// path and reports completion via editorFinishedMsg.
func runExternalEditor(path string) tea.Cmd {
	editor := resolveEditor()
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
		return entityKeyAt(m.laws, index, func(law domain.Law) string { return law.Key })
	case entityKindPersona:
		return entityKeyAt(m.personas, index, func(persona domain.Persona) string { return persona.Key })
	case entityKindSkill:
		return entityKeyAt(m.skills, index, func(skill domain.Skill) string { return skill.Key })
	case entityKindTemplate:
		return entityKeyAt(m.templates, index, func(template config.TaskTemplate) string { return template.Slug })
	case entityKindTag:
		return entityKeyAt(m.tags, index, func(tag domain.Tag) string { return tag.Name })
	}
	return ""
}

func entityKeyAt[T any](items []T, index int, key func(T) string) string {
	if index < 0 || index >= len(items) {
		return ""
	}
	return key(items[index])
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

// handleEditorFinished is the post-editor callback. Reloads the bundle
// through the editor, rotates the per-project Snapshot (in production
// the BundleCache.Reload path; in tests the inline override on
// Repositories.Snapshot), and refreshes model state so the freshly
// written file is reflected.
func (m *Model) handleEditorFinished(msg editorFinishedMsg) {
	if msg.err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.editor_error_fmt"), msg.err.Error())
		return
	}
	if m.repos.Editor != nil {
		_, err := bundledraft.ApplyPlanned(m.ctx, m.repos.Editor, nil)
		if err != nil {
			m.status = err.Error()
			return
		}
		if err := m.rotateSnapshotAfterEdit(); err != nil {
			m.status = err.Error()
			return
		}
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.status = m.t("tui.status.saved")
}

// rotateSnapshotAfterEdit reloads the project runtime after an entity write.
func (m *Model) rotateSnapshotAfterEdit() error {
	if m.repos.Cache == nil {
		return fmt.Errorf("project runtime is unavailable")
	}
	if _, err := m.repos.Cache.ReloadView(m.ctx, m.repos.ProjectID, m.repos.ConfigPath); err != nil {
		return err
	}
	m.studioRuntimeGeneration++
	return nil
}

func (m *Model) deleteEntity(kind entityKind, slug string) {
	if m.repos.Editor == nil {
		m.status = m.t("tui.status.editor_unavailable")
		return
	}
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	var err error
	switch kind {
	case entityKindLaw:
		_, err = svc.RemoveLaw(m.ctx, slug)
	case entityKindSkill:
		_, err = svc.RemoveSkill(m.ctx, slug)
	case entityKindPersona:
		_, err = svc.RemovePersona(m.ctx, slug)
	}
	if err != nil {
		m.status = err.Error()
		return
	}
	if err := m.rotateSnapshotAfterEdit(); err != nil {
		m.status = err.Error()
		return
	}
	m.clearDeletePrompt("")
	if refreshErr := m.refresh(); refreshErr != nil {
		m.status = refreshErr.Error()
		return
	}
	if len(m.screenStack) > 0 && m.screenStack[len(m.screenStack)-1] == screenhost.EntityDetail && m.entityDetailScreen.Payload().Slug == slug {
		m.popScreen()
		m.status = m.t("tui.status.deleted")
		return
	}
	m.status = m.t("tui.status.deleted")
}

func (m *Model) prepareTagDelete(name string) {
	for _, tag := range m.tags {
		if tag.Name != name {
			continue
		}
		if tag.UsageCount > 0 {
			if screen, ok := m.entityListScreens[screenhost.SettingsTags]; ok {
				m.entityListScreens[screenhost.SettingsTags] = screen.CancelDelete()
			}
			m.status = fmt.Sprintf(m.t("tui.status.tag_in_use_fmt"), tag.Label, tag.UsageCount)
			return
		}
		m.status = fmt.Sprintf(m.t("tui.confirm.tag_delete_fmt"), tag.Label)
		return
	}
	m.status = m.t("tui.status.nothing_to_delete")
}

func (m *Model) confirmTagDelete(name string) {
	for _, tag := range m.tags {
		if tag.Name == name && tag.UsageCount > 0 {
			m.prepareTagDelete(name)
			return
		}
	}
	m.deleteTagByName(name)
}

func (m *Model) deleteOrphanTags() {
	if m.repos.Tags == nil {
		m.status = m.t("tui.status.tag_repo_unavailable")
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
		m.status = m.t("tui.status.no_orphan_tags")
	} else {
		m.status = fmt.Sprintf(m.t("tui.status.orphan_tags_deleted_fmt"), n)
	}
}

func (m *Model) deleteTagByName(name string) {
	if m.repos.Tags == nil {
		m.status = m.t("tui.status.tag_repo_unavailable")
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
		m.status = fmt.Sprintf(m.t("tui.status.tag_deleted_fmt"), name)
	}
}

func (m *Model) prepareTagMerge(name string) {
	for _, tag := range m.tags {
		if tag.Name != name {
			continue
		}
		m.status = fmt.Sprintf(m.t("tui.confirm.tag_merge_fmt"), tag.Label)
		return
	}
	if screen, ok := m.entityListScreens[screenhost.SettingsTags]; ok {
		m.entityListScreens[screenhost.SettingsTags] = screen.CancelMerge()
	}
	m.status = m.t("tui.status.tag_merge_missing")
}

func (m *Model) confirmTagMerge(sourceName, targetName string) {
	svc := m.repos.operationService()
	if svc == nil {
		m.status = m.t("tui.status.tag_merge_unavailable")
		if screen, ok := m.entityListScreens[screenhost.SettingsTags]; ok {
			m.entityListScreens[screenhost.SettingsTags] = screen.CancelMerge()
		}
		return
	}
	if sourceName == "" || targetName == "" || sourceName == targetName {
		m.status = m.t("tui.status.tag_merge_same")
		return
	}
	var sourceID, targetID int64
	var sourceLabel, targetLabel string
	for _, tag := range m.tags {
		switch tag.Name {
		case sourceName:
			sourceID, sourceLabel = tag.ID, tag.Label
		case targetName:
			targetID, targetLabel = tag.ID, tag.Label
		}
	}
	if sourceID == 0 || targetID == 0 {
		m.status = m.t("tui.status.tag_merge_missing")
		return
	}
	resp, err := svc.MergeTags(m.ctx, contract.MergeTagsInput{
		SourceTagID: sourceID,
		TargetTagID: targetID})
	if err != nil {
		m.status = err.Error()
		return
	}
	if screen, ok := m.entityListScreens[screenhost.SettingsTags]; ok {
		m.entityListScreens[screenhost.SettingsTags] = screen.CancelArmed()
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	label := resp.Tag.Label
	if label == "" {
		label = targetLabel
	}
	if sourceLabel == "" {
		sourceLabel = sourceName
	}
	m.status = fmt.Sprintf(m.t("tui.status.tag_merged_fmt"), sourceLabel, label)
}

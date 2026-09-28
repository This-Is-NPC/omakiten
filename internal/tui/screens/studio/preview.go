package studio

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	studioprojection "omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

// commandPreviewCache holds the last ResolveCommand markdown so Commands
// paint never concatenates persona+skills+laws+templates on the TUI thread.
// The pointer is shared across Screen copies (same contract as studioDraft).
type commandPreviewCache struct {
	projectID         int64
	runtimeGeneration uint64
	name              string
	spec              config.CommandSpec
	markdown          string
	// formatted is the markdown after sanitising, which is what the zone paints.
	// It is stored rather than derived because sanitising walks every rune of the
	// document and the zone re-derives it on every render — two or three times a
	// keystroke, on a prompt that is thousands of lines. It was the largest cost
	// left in a Commands keystroke once the composition itself was memoised.
	formatted string
	err       error
	ready     bool
	gen       uint64
}

type commandPreviewMsg struct {
	projectID         int64
	runtimeGeneration uint64
	gen               uint64
	name              string
	spec              config.CommandSpec
	markdown          string
	err               error
}

func cloneCommandSpec(spec config.CommandSpec) config.CommandSpec {
	spec.Laws = slices.Clone(spec.Laws)
	spec.LawsDisabled = slices.Clone(spec.LawsDisabled)
	spec.Templates = slices.Clone(spec.Templates)
	spec.Skills = slices.Clone(spec.Skills)
	return spec
}

func commandSpecEq(a, b config.CommandSpec) bool {
	return a.Persona == b.Persona &&
		slices.Equal(a.Laws, b.Laws) &&
		slices.Equal(a.LawsDisabled, b.LawsDisabled) &&
		slices.Equal(a.Templates, b.Templates) &&
		slices.Equal(a.Skills, b.Skills)
}

func (m Screen) studioPreviewTarget() (name string, spec config.CommandSpec, bundle config.Bundle, ok bool) {
	bundle, _ = m.studioCommandsBundle()
	rows := studioCommandRows(bundle.Commands, m.knownCommandNames())
	if len(rows) == 0 {
		return "", config.CommandSpec{}, bundle, false
	}
	m.studioCommandsClamp(len(rows))
	row := rows[m.studioCommandIndex]
	return row.Key, row.Spec, bundle, true
}

func (c *commandPreviewCache) matches(projectID int64, runtimeGeneration uint64, name string, spec config.CommandSpec) bool {
	return c != nil && c.ready && c.projectID == projectID && c.runtimeGeneration == runtimeGeneration && c.name == name && commandSpecEq(c.spec, spec)
}

func (m Screen) renderStudioPromptPreview(bundle config.Bundle) string {
	rows := studioCommandRows(bundle.Commands, m.knownCommandNames())
	m.studioCommandsClamp(len(rows))
	if len(rows) == 0 {
		return m.tr("tui.studio.preview.prompt_empty", "No command bindings available.")
	}
	row := rows[m.studioCommandIndex]
	if m.commandPreview.matches(m.repos.ProjectID, m.repos.RuntimeGeneration, row.Key, row.Spec) {
		return m.commandPreview.formatted
	}
	if m.repos.ResolveCommand == nil {
		return ""
	}
	return m.tr("tui.studio.commands.preview_composing", "Composing preview…")
}

func (m Screen) renderPreparedStudioPromptPreview(row studioprojection.CommandRow) string {
	if m.commandPreview.matches(m.repos.ProjectID, m.repos.RuntimeGeneration, row.Key, row.Spec) {
		return m.commandPreview.formatted
	}
	if m.repos.ResolveCommand == nil {
		return ""
	}
	return m.tr("tui.studio.commands.preview_composing", "Composing preview…")
}

func (m Screen) formatCommandPreview(markdown string, err error) string {
	if err != nil {
		return m.tr("tui.studio.preview.prompt_error", "%s", screenkit.Sanitize(err.Error()))
	}
	return screenkit.SanitizeMultiline(strings.TrimRight(markdown, "\n"))
}

func (m Screen) PromptPreview(frame screenhost.Frame, bundle config.Bundle) string {
	m = m.withFrame(frame)
	return m.resolvedStudioPromptPreview(bundle)
}

func (m Screen) resolvedStudioPromptPreview(bundle config.Bundle) string {
	rows := studioCommandRows(bundle.Commands, m.knownCommandNames())
	m.studioCommandsClamp(len(rows))
	if len(rows) == 0 {
		return m.tr("tui.studio.preview.prompt_empty", "No command bindings available.")
	}
	row := rows[m.studioCommandIndex]
	if m.commandPreview.matches(m.repos.ProjectID, m.repos.RuntimeGeneration, row.Key, row.Spec) {
		return m.commandPreview.formatted
	}
	md, err := m.resolveStudioCandidateCommand(bundle, row.Key)
	m.storeCommandPreview(row.Key, row.Spec, md, err)
	return m.formatCommandPreview(md, err)
}

func (m Screen) commandPreviewCmd() tea.Cmd {
	if m.repos.ResolveCommand == nil || m.commandPreview == nil {
		return nil
	}
	name, spec, bundle, ok := m.studioPreviewTarget()
	if !ok || m.commandPreview.matches(m.repos.ProjectID, m.repos.RuntimeGeneration, name, spec) {
		return nil
	}
	m.commandPreview.gen++
	gen := m.commandPreview.gen
	resolve := m.repos.ResolveCommand
	spec = cloneCommandSpec(spec)
	return func() tea.Msg {
		md, err := resolve(bundle, name)
		return commandPreviewMsg{projectID: m.repos.ProjectID, runtimeGeneration: m.repos.RuntimeGeneration, gen: gen, name: name, spec: spec, markdown: md, err: err}
	}
}

func (m Screen) applyCommandPreview(msg commandPreviewMsg) {
	if m.commandPreview == nil || msg.projectID != m.repos.ProjectID || msg.runtimeGeneration != m.repos.RuntimeGeneration || msg.gen != m.commandPreview.gen {
		return
	}
	m.storeCommandPreview(msg.name, msg.spec, msg.markdown, msg.err)
}

func (m Screen) storeCommandPreview(name string, spec config.CommandSpec, markdown string, err error) {
	if m.commandPreview == nil {
		return
	}
	m.commandPreview.name = name
	m.commandPreview.projectID = m.repos.ProjectID
	m.commandPreview.runtimeGeneration = m.repos.RuntimeGeneration
	m.commandPreview.spec = cloneCommandSpec(spec)
	m.commandPreview.markdown = markdown
	m.commandPreview.formatted = m.formatCommandPreview(markdown, err)
	m.commandPreview.err = err
	m.commandPreview.ready = true
}

// studioCandidateCommands is the command map the prompt preview clamps its
// cursor against, read straight off the held candidate.
func (m Screen) studioCandidateCommands() map[string]config.CommandSpec {
	if m.studioDraft != nil {
		return m.studioDraft.Candidate().Commands
	}
	if snap := m.repos.activeSnapshot(); snap != nil {
		return snap.Commands()
	}
	return nil
}

// studioCandidateDirty reports whether the held candidate carries unsaved
// edits, read straight off the draft the screen opened at entry.
func (m Screen) studioCandidateDirty() bool {
	return m.studioDraft != nil && m.studioDraft.Dirty()
}

func (m Screen) resolveStudioCandidateCommand(bundle config.Bundle, name string) (string, error) {
	if m.repos.ResolveCommand == nil {
		return "", nil
	}
	return m.repos.ResolveCommand(bundle, name)
}

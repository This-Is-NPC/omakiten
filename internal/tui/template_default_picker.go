package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
)

// defaultPickerOption is one row in the template default picker. Kind is the
// `default:` value to write; ProjectSlug optionally scopes it. Empty Kind
// represents the "(none)" row that clears the binding.
type defaultPickerOption struct {
	Kind        string
	ProjectSlug string
}

// label renders the visible text for a row, e.g. "task (global)" or
// "task (project: omakiten)".
func (o defaultPickerOption) label() string {
	if o.Kind == "" {
		return "(none)"
	}
	scope := "global"
	if o.ProjectSlug != "" {
		scope = "project: " + o.ProjectSlug
	}
	return fmt.Sprintf("%s (%s)", o.Kind, scope)
}

func (m *Model) openTemplateDefaultPickerForSelected() {
	if m.entityCount(entityKindTemplate) == 0 {
		m.status = "No template selected"
		return
	}
	cursor := m.selectedEntityIndex(entityKindTemplate)
	slug := m.entitySlugAt(entityKindTemplate, cursor)
	if slug == "" {
		return
	}
	m.openTemplateDefaultPicker(slug)
}

func (m *Model) openTemplateDefaultPicker(slug string) {
	template, ok := m.findTemplateBySlug(slug)
	if !ok {
		m.status = "Template not found"
		return
	}

	options := buildTemplateDefaultOptions(m.repos.Editor, m.project.Slug)
	cursor := selectedDefaultOptionIndex(options, template.Default, template.ProjectSlug)

	m.entityScreen = entityScreenView
	m.entityForm = entityForm{
		kind:         entityKindTemplate,
		mode:         entityScreenDefaultPicker,
		slug:         slug,
		pickerCursor: cursor,
	}
	m.pickerScroll = 0
	m.status = "Default picker"
}

// buildTemplateDefaultOptions enumerates kind × {global, current-project}
// combinations from config.template_defaults plus a final "(none)" row. The
// active project provides the project-scoped option; when the model has no
// active project (rare in practice) only the global rows render.
func buildTemplateDefaultOptions(editor *app.BundleEditor, activeProject string) []defaultPickerOption {
	var kinds []string
	if editor != nil {
		if bundle, err := editor.Load(); err == nil {
			kinds = bundle.Config.TemplateKinds()
		}
	}
	if len(kinds) == 0 {
		kinds = append(kinds, config.DefaultTemplateKinds...)
	}
	options := make([]defaultPickerOption, 0, len(kinds)*2+1)
	for _, kind := range kinds {
		options = append(options, defaultPickerOption{Kind: kind})
		if activeProject != "" {
			options = append(options, defaultPickerOption{Kind: kind, ProjectSlug: activeProject})
		}
	}
	options = append(options, defaultPickerOption{}) // (none)
	return options
}

func selectedDefaultOptionIndex(options []defaultPickerOption, currentKind, currentProject string) int {
	for i, opt := range options {
		if opt.Kind == currentKind && opt.ProjectSlug == currentProject {
			return i
		}
	}
	// Fall back to (none) when the current binding is not in the visible
	// option set (e.g. the kind was removed from template_defaults).
	for i, opt := range options {
		if opt.Kind == "" {
			return i
		}
	}
	return 0
}

func (m Model) updateTemplateDefaultPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	options := buildTemplateDefaultOptions(m.repos.Editor, m.project.Slug)
	rowCount := len(options)
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.closeEntityScreen("Default picker cancelled")
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
		m.syncPickerScroll(rowCount)
	case "home", "g":
		m.entityForm.pickerCursor = 0
		m.syncPickerScroll(rowCount)
	case "end", "G":
		m.entityForm.pickerCursor = rowCount - 1
		m.syncPickerScroll(rowCount)
	case "enter":
		if m.entityForm.pickerCursor < 0 || m.entityForm.pickerCursor >= rowCount {
			return m, nil
		}
		chosen := options[m.entityForm.pickerCursor]
		if err := m.applyTemplateDefault(m.entityForm.slug, chosen); err != nil {
			m.status = err.Error()
			return m, nil
		}
		if err := m.refresh(); err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.closeEntityScreen(fmt.Sprintf("Template %s ← default %s", m.entityForm.slug, chosen.label()))
	}
	return m, nil
}

func (m Model) renderTemplateDefaultPicker() string {
	options := buildTemplateDefaultOptions(m.repos.Editor, m.project.Slug)
	contentWidth := m.availableWidth() - 4

	template, _ := m.findTemplateBySlug(m.entityForm.slug)
	currentKind, currentProject := template.Default, template.ProjectSlug

	rows := make([]string, 0, len(options))
	for index, opt := range options {
		marker := normalMarker
		if m.entityForm.pickerCursor == index {
			marker = m.styles.marker.Render(selectionMarker)
		}
		dot := " "
		if opt.Kind == currentKind && opt.ProjectSlug == currentProject {
			dot = "•"
		}
		rows = append(rows, fmt.Sprintf("%s %s %s", marker, dot, opt.label()))
	}
	header := []string{
		m.styles.kicker(fmt.Sprintf("Default kind for template · %s", m.entityForm.slug)),
		m.styles.hint.Render("up/down: move · enter: assign (clears any other template with the same default) · esc: cancel"),
		"",
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	header = append(header, m.sliceScrollRows(rows, m.pickerScroll, m.pickerViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(header, "\n")), 2)
}

// applyTemplateDefault writes the chosen kind/project into the focused
// template's frontmatter and atomically clears the same default from any
// other template that previously held it. Single BundleEditor.ApplyWithFiles
// call: failures roll back both the file edits and the wiring (no half
// assignments left on disk).
func (m *Model) applyTemplateDefault(slug string, chosen defaultPickerOption) error {
	if m.repos.Editor == nil {
		return fmt.Errorf("editor not available")
	}
	bundle, err := m.repos.Editor.Load()
	if err != nil {
		return err
	}
	target, found := findTemplateInBundle(bundle, slug)
	if !found {
		return fmt.Errorf("template %q not found", slug)
	}

	// Plan the file ops: rewrite `target` with the new default/project; for
	// any sibling template currently bound to the same (kind, project),
	// rewrite it to clear the binding.
	ops := []app.FileOp{}
	updated, err := rewriteTemplateFrontmatter(target.SourcePath, chosen.Kind, chosen.ProjectSlug)
	if err != nil {
		return err
	}
	ops = append(ops, app.FileOp{Op: app.OpWrite, Path: target.SourcePath, Bytes: updated})

	if chosen.Kind != "" {
		for _, sibling := range bundle.Templates {
			if sibling.Slug == slug {
				continue
			}
			if sibling.Default == chosen.Kind && sibling.ProjectSlug == chosen.ProjectSlug {
				cleared, err := rewriteTemplateFrontmatter(sibling.SourcePath, "", "")
				if err != nil {
					return err
				}
				ops = append(ops, app.FileOp{Op: app.OpWrite, Path: sibling.SourcePath, Bytes: cleared})
			}
		}
	}

	_, err = m.repos.Editor.ApplyWithFiles(m.ctx, nil, ops)
	return err
}

func findTemplateInBundle(bundle config.Bundle, slug string) (config.TaskTemplate, bool) {
	for _, t := range bundle.Templates {
		if t.Slug == slug {
			return t, true
		}
	}
	return config.TaskTemplate{}, false
}

// rewriteTemplateFrontmatter loads the template file, sets/clears the
// `default:` and `project:` fields in the frontmatter, and returns the new
// file bytes. Other frontmatter keys, the body, and ordering are preserved
// so user-authored formatting survives the round-trip.
func rewriteTemplateFrontmatter(path, kind, projectSlug string) ([]byte, error) {
	raw, err := readTemplateFile(path)
	if err != nil {
		return nil, err
	}
	fm, body, err := config.SplitFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	lines := strings.Split(strings.TrimRight(string(fm), "\n"), "\n")
	wroteDefault := false
	wroteProject := false
	out := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(stripped, "default:"):
			if kind != "" {
				out = append(out, fmt.Sprintf("default: %s", kind))
				wroteDefault = true
			}
			// kind=="" → drop the line (clears the binding)
		case strings.HasPrefix(stripped, "project:"):
			if projectSlug != "" {
				out = append(out, fmt.Sprintf("project: %s", projectSlug))
				wroteProject = true
			}
		default:
			out = append(out, line)
		}
	}
	if kind != "" && !wroteDefault {
		out = append(out, fmt.Sprintf("default: %s", kind))
	}
	if projectSlug != "" && !wroteProject {
		out = append(out, fmt.Sprintf("project: %s", projectSlug))
	}

	return config.JoinFrontmatter([]byte(strings.Join(out, "\n")+"\n"), body), nil
}

// readTemplateFile is a thin wrapper around os.ReadFile factored out for
// stubbing in tests; production reads straight from disk.
var readTemplateFile = func(path string) ([]byte, error) {
	return os.ReadFile(path)
}

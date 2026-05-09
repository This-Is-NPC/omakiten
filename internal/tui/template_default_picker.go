package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/tui/components/picker"
)

// defaultPickerOption is one row in the template default picker. Kind is
// the default value to write; the binding is implicitly project-scoped to
// the active project (the TUI always runs within one). Empty Kind is the
// "(none)" row that clears the binding entirely.
type defaultPickerOption struct {
	Kind string
}

func (o defaultPickerOption) label() string {
	if o.Kind == "" {
		return "(none)"
	}
	return o.Kind
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
	if m.project.Slug == "" {
		m.status = "TUI must be opened inside a project to assign a template default"
		return
	}

	options := buildTemplateDefaultOptions(m.repos.Editor)
	cursor := selectedDefaultOptionIndex(options, template.Default, template.ProjectSlug, m.project.Slug)

	m.entityScreen = entityScreenView
	m.entityForm = entityForm{
		kind: entityKindTemplate,
		mode: entityScreenDefaultPicker,
		slug: slug,
	}
	m.entityPicker = picker.New(picker.Single)
	m.entityPicker.Cursor = cursor
	m.status = "Default picker"
}

// buildTemplateDefaultOptions enumerates the kinds the user can claim from
// `config.template_defaults`, plus a trailing "(none)" row that clears the
// binding. Project scope is implicit (current project) — the TUI is always
// opened inside a project, so no global/project toggle is needed.
func buildTemplateDefaultOptions(editor *app.BundleEditor) []defaultPickerOption {
	// Validator guarantees template_defaults is non-empty in the
	// loaded bundle. When the editor is nil (test contexts) we can't
	// load — return empty kinds; callers handle the empty list.
	var kinds []string
	if editor != nil {
		if bundle, err := editor.Load(); err == nil {
			kinds = bundle.Config.TemplateKinds()
		}
	}
	options := make([]defaultPickerOption, 0, len(kinds)+1)
	for _, kind := range kinds {
		options = append(options, defaultPickerOption{Kind: kind})
	}
	options = append(options, defaultPickerOption{}) // (none)
	return options
}

// selectedDefaultOptionIndex picks the option that matches the template's
// current binding, but only when the binding is actually project-scoped to
// the active project. A global binding (project="") on a custom template is
// unusual but not invalid — the picker treats it as no project override and
// lands on (none).
func selectedDefaultOptionIndex(options []defaultPickerOption, currentKind, currentProject, activeProject string) int {
	if currentKind != "" && currentProject == activeProject {
		for i, opt := range options {
			if opt.Kind == currentKind {
				return i
			}
		}
	}
	for i, opt := range options {
		if opt.Kind == "" {
			return i
		}
	}
	return 0
}

func (m Model) updateTemplateDefaultPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m, tea.Quit
	}
	options := buildTemplateDefaultOptions(m.repos.Editor)
	rowCount := len(options)
	var cmd tea.Cmd
	m.entityPicker, cmd = m.entityPicker.Update(msg, rowCount, scrollDataRows(m.pickerViewportRows()))
	switch m.entityPicker.LastEvent() {
	case picker.EventCancel:
		m.closeEntityScreen("Default picker cancelled")
	case picker.EventSelect:
		if m.entityPicker.Cursor < 0 || m.entityPicker.Cursor >= rowCount {
			return m, cmd
		}
		chosen := options[m.entityPicker.Cursor]
		if err := m.applyTemplateDefault(m.entityForm.slug, chosen.Kind, m.project.Slug); err != nil {
			m.status = err.Error()
			return m, cmd
		}
		if err := m.refresh(); err != nil {
			m.status = err.Error()
			return m, cmd
		}
		if chosen.Kind == "" {
			m.closeEntityScreen(fmt.Sprintf("Template %s · default cleared", m.entityForm.slug))
		} else {
			m.closeEntityScreen(fmt.Sprintf("Template %s · default %q for project %s", m.entityForm.slug, chosen.Kind, m.project.Slug))
		}
	}
	return m, cmd
}

func (m Model) renderTemplateDefaultPicker() string {
	options := buildTemplateDefaultOptions(m.repos.Editor)

	template, _ := m.findTemplateBySlug(m.entityForm.slug)
	// Mark the option that matches the template's current binding for the
	// active project. A global or different-project binding shows nothing
	// marked — the picker is exclusively for the current project's scope.
	currentKind := ""
	if template.Default != "" && template.ProjectSlug == m.project.Slug {
		currentKind = template.Default
	}

	rows := make([]string, 0, len(options))
	for index, opt := range options {
		marker := m.cursorMarker(m.entityPicker.Cursor == index)
		dot := " "
		if opt.Kind == currentKind {
			dot = "•"
		}
		rows = append(rows, fmt.Sprintf("%s %s %s", marker, dot, opt.label()))
	}
	header := []string{
		m.styles.kicker(fmt.Sprintf("Default kind · template %s · project %s", m.entityForm.slug, m.project.Slug)),
		m.styles.hint.Render("up/down: move · enter: assign for this project (clears prior owner) · esc: cancel"),
		"",
	}
	return m.renderPickerPanel(header, rows, m.entityPicker.Scroll, m.pickerViewportRows())
}

// applyTemplateDefault delegates to app.TemplateService, which owns the
// file/wiring transactional sequence. Local rewriting helpers used to live
// here; they were promoted into internal/app/template_service.go so the
// behavior has its own test surface and the TUI stays free of bundle I/O.
func (m *Model) applyTemplateDefault(slug, kind, projectSlug string) error {
	return app.NewTemplateService(m.repos.Editor, m.repos.EntityFiles).SetDefault(m.ctx, slug, kind, projectSlug)
}

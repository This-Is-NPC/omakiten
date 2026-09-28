package tui

import (
	"fmt"

	"omakiten/internal/relationshipprojection"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/relationshippicker"
)

func (m *Model) openTemplateDefaultPicker(slug string) {
	template, ok := m.findTemplateBySlug(slug)
	if !ok {
		m.status = m.t("tui.status.template_not_found")
		return
	}
	if m.project.Slug == "" {
		m.status = m.t("tui.status.template_picker_needs_project")
		return
	}
	options := buildTemplateDefaultOptions(m.repos.Editor, template.Default, template.ProjectSlug, m.project.Slug)
	m.relationshipPickerGeneration++
	m.templateDefaultScreen = relationshippicker.New(relationshippicker.TemplateDefault).Open(relationshippicker.Payload{
		Kind: relationshippicker.TemplateDefault, EntitySlug: slug, ProjectSlug: m.project.Slug,
		Generation: m.relationshipPickerGeneration, Options: options})
	m.status = m.t("tui.status.default_picker")
	m.pushScreen(screenhost.TemplateDefault)
}

func buildTemplateDefaultOptions(editor BundleEditor, currentKind, currentProject, activeProject string) []relationshippicker.Option {
	var kinds []string
	if editor != nil {
		if bundle, err := editor.Load(); err == nil {
			kinds = bundle.Config.TemplateKinds()
		}
	}
	return relationshipprojection.TemplateDefaultOptions(kinds, currentKind, currentProject, activeProject)
}

func (m *Model) saveTemplateDefault(action screenhost.Action) {
	screen := m.templateDefaultScreen
	valid := false
	for _, option := range screen.Options() {
		if option.Value == action.Value && (action.Value != "" || option.None) {
			valid = true
			break
		}
	}
	if !valid {
		return
	}
	slug := screen.Payload().EntitySlug
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	if err := svc.SetTemplateDefault(m.ctx, slug, action.Value, m.project.Slug); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.popScreen()
	if action.Value == "" {
		m.status = fmt.Sprintf(m.t("tui.status.template_default_cleared_fmt"), slug)
	} else {
		m.status = fmt.Sprintf(m.t("tui.status.template_default_set_fmt"), slug, action.Value, m.project.Slug)
	}
	m.refreshStackedEntityDetail(entityKindTemplate)
}

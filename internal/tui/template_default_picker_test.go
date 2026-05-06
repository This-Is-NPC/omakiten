package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
)

// Custom-only fixture: a default global template ships at the root, and
// the user adds a custom override under templates/custom/. The picker key
// `a` is gated to customs only.
func writeCustomTemplate(t *testing.T, root, slug, body string) {
	t.Helper()
	customDir := filepath.Join(root, "templates", "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(customDir, slug+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestTemplateDefaultPickerAssignsProjectScopedAndClearsPrior(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	root := model.repos.Editor.RootDir()
	writeCustomTemplate(t, root, "task-mine",
		"---\nname: My Task Template\nentity: task\n---\nbody\n")
	if _, err := model.repos.Editor.Apply(model.ctx, nil); err != nil {
		t.Fatalf("Apply() reload error = %v", err)
	}
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	// Focus the custom template.
	model.entityKind = entityKindTemplate
	if model.entityCursors == nil {
		model.entityCursors = map[entityKind]int{}
	}
	target := -1
	for i, tpl := range model.templates {
		if tpl.Slug == "task-mine" {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("custom template task-mine missing from refresh")
	}
	model.entityCursors[entityKindTemplate] = target

	model.openTemplateDefaultPickerForSelected()
	if model.entityForm.mode != entityScreenDefaultPicker {
		t.Fatalf("picker should open on a custom template, mode = %v / status = %q", model.entityForm.mode, model.status)
	}

	// Pick the "task" kind (always first option in the canonical list).
	options := buildTemplateDefaultOptions(model.repos.Editor)
	idx := -1
	for i, opt := range options {
		if opt.Kind == "task" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("test setup: no task option in picker")
	}
	model.entityForm.pickerCursor = idx

	got, _ := model.updateTemplateDefaultPicker(tea.KeyMsg{Type: tea.KeyEnter})
	updated := got.(Model)

	// task-mine should own the project-scoped default for the active project.
	bundle, err := updated.repos.Editor.Load()
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	bound := bundle.TemplateByDefault("task", updated.project.Slug)
	if bound == nil || bound.Slug != "task-mine" {
		t.Fatalf("task-mine should own default=task for project %s, got %+v", updated.project.Slug, bound)
	}
	if bound.ProjectSlug != updated.project.Slug {
		t.Fatalf("binding ProjectSlug = %q, want %q", bound.ProjectSlug, updated.project.Slug)
	}
	// The original global task-default binding must remain untouched —
	// pressing `a` only mutates the project-scoped slot.
	globalBound := bundle.TemplateByDefault("task", "")
	if globalBound == nil || globalBound.Slug != "task-default" {
		t.Fatalf("global task default unexpectedly changed: %+v", globalBound)
	}
}

func TestTemplateDefaultPickerRejectsGlobalTemplates(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	model.entityKind = entityKindTemplate
	if model.entityCursors == nil {
		model.entityCursors = map[entityKind]int{}
	}
	for i, tpl := range model.templates {
		if tpl.Slug == "task-default" { // global template (root, not custom/)
			model.entityCursors[entityKindTemplate] = i
			break
		}
	}

	model.openTemplateDefaultPickerForSelected()
	if model.entityForm.mode == entityScreenDefaultPicker {
		t.Fatal("picker should NOT open on global templates")
	}
	if model.status == "" {
		t.Fatal("expected status hint explaining why picker is gated")
	}
}

func TestTemplateDefaultPickerNoneClearsProjectBinding(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	root := model.repos.Editor.RootDir()
	// Custom template starts already bound to (task, current-project).
	writeCustomTemplate(t, root, "task-mine",
		fmt.Sprintf("---\nname: My Task\nentity: task\ndefault: task\nproject: %s\n---\nbody\n", model.project.Slug))
	if _, err := model.repos.Editor.Apply(model.ctx, nil); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	model.entityKind = entityKindTemplate
	if model.entityCursors == nil {
		model.entityCursors = map[entityKind]int{}
	}
	for i, tpl := range model.templates {
		if tpl.Slug == "task-mine" {
			model.entityCursors[entityKindTemplate] = i
			break
		}
	}

	model.openTemplateDefaultPickerForSelected()
	options := buildTemplateDefaultOptions(model.repos.Editor)
	model.entityForm.pickerCursor = len(options) - 1 // (none)

	got, _ := model.updateTemplateDefaultPicker(tea.KeyMsg{Type: tea.KeyEnter})
	updated := got.(Model)

	bundle, err := updated.repos.Editor.Load()
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if got := bundle.TemplateByDefault("task", updated.project.Slug); got == nil || got.Slug != "task-default" {
		t.Fatalf("after (none) the project should fall back to global task-default, got %+v", got)
	}
}

func TestTemplateDefaultPickerOptionsAreKindOnly(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	options := buildTemplateDefaultOptions(model.repos.Editor)
	if len(options) == 0 {
		t.Fatal("expected at least the (none) option")
	}
	if last := options[len(options)-1]; last.Kind != "" {
		t.Fatalf("last option should be (none), got %+v", last)
	}
	// Verify each kind appears exactly once (no per-project duplicates).
	seen := map[string]int{}
	for _, opt := range options {
		seen[opt.Kind]++
	}
	for kind, count := range seen {
		if count > 1 {
			t.Fatalf("kind %q listed %d times — picker should be flat", kind, count)
		}
	}
}

// Sanity: build helper exercises live config so the picker resolves
// template_defaults from the loaded bundle, not a hardcoded list.
func TestTemplateDefaultPickerHonorsConfigTemplateDefaults(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	if _, err := model.repos.Editor.Apply(model.ctx, func(bundle *config.Bundle) error {
		bundle.Config.TemplateDefaults = []string{"task"}
		return nil
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	options := buildTemplateDefaultOptions(model.repos.Editor)
	for _, opt := range options {
		if opt.Kind == "pr" {
			t.Fatalf("config.template_defaults restricted to [task] but pr appeared: %+v", options)
		}
	}
}

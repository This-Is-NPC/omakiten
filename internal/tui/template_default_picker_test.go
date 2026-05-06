package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
)

func TestTemplateDefaultPickerAssignsAndClearsPrior(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	root := model.repos.Editor.RootDir()

	// Sanity: task-default starts with default=task in its frontmatter
	// (set by newEntityModelWithTemplates fixture).
	bundle, err := model.repos.Editor.Load()
	if err != nil {
		t.Fatalf("editor.Load() error = %v", err)
	}
	if got := bundle.TemplateByDefault("task", ""); got == nil || got.Slug != "task-default" {
		t.Fatalf("initial task-default not bound: %+v", got)
	}

	// Assign default=task to task-bug; task-default's binding must clear so
	// the (kind=task, project="") slot has a unique owner.
	model.entityKind = entityKindTemplate
	if model.entityCursors == nil {
		model.entityCursors = map[entityKind]int{}
	}
	for i, tpl := range model.templates {
		if tpl.Slug == "task-bug" {
			model.entityCursors[entityKindTemplate] = i
			break
		}
	}

	model.openTemplateDefaultPickerForSelected()
	if model.entityForm.mode != entityScreenDefaultPicker {
		t.Fatalf("expected default picker open, got %v", model.entityForm.mode)
	}

	// Cursor should already be on the (none) row since task-bug had no
	// binding. Walk up to the "task (global)" entry and apply.
	options := buildTemplateDefaultOptions(model.repos.Editor, model.project.Slug)
	target := -1
	for i, opt := range options {
		if opt.Kind == "task" && opt.ProjectSlug == "" {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("test setup: no task/global option in picker")
	}
	model.entityForm.pickerCursor = target

	got, _ := model.updateTemplateDefaultPicker(tea.KeyMsg{Type: tea.KeyEnter})
	updated := got.(Model)
	if updated.entityForm.mode != entityScreenClosed {
		t.Fatalf("picker should close after assign, mode = %v", updated.entityForm.mode)
	}

	// Reload the bundle from disk and check both files.
	bundle2, err := updated.repos.Editor.Load()
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	bound := bundle2.TemplateByDefault("task", "")
	if bound == nil || bound.Slug != "task-bug" {
		t.Fatalf("task-bug should now own default=task, got %+v", bound)
	}
	for _, tpl := range bundle2.Templates {
		if tpl.Slug == "task-default" && tpl.Default != "" {
			t.Fatalf("task-default should be cleared (no default), got %q", tpl.Default)
		}
	}

	// Inspect the disk files directly to make sure frontmatter was rewritten.
	taskBugPath := filepath.Join(root, "templates", "task-bug.md")
	bugBytes, err := os.ReadFile(taskBugPath)
	if err != nil {
		t.Fatalf("read task-bug error = %v", err)
	}
	if !strings.Contains(string(bugBytes), "default: task") {
		t.Fatalf("task-bug frontmatter missing default: task\n%s", bugBytes)
	}

	taskDefaultPath := filepath.Join(root, "templates", "task-default.md")
	defaultBytes, err := os.ReadFile(taskDefaultPath)
	if err != nil {
		t.Fatalf("read task-default error = %v", err)
	}
	if strings.Contains(string(defaultBytes), "default: task") {
		t.Fatalf("task-default frontmatter still has default: task — clear-then-set failed\n%s", defaultBytes)
	}
}

func TestTemplateDefaultPickerNoneClearsBinding(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	model.entityKind = entityKindTemplate
	if model.entityCursors == nil {
		model.entityCursors = map[entityKind]int{}
	}
	// Focus task-default — it currently owns default: task.
	for i, tpl := range model.templates {
		if tpl.Slug == "task-default" {
			model.entityCursors[entityKindTemplate] = i
			break
		}
	}

	model.openTemplateDefaultPickerForSelected()
	options := buildTemplateDefaultOptions(model.repos.Editor, model.project.Slug)
	noneIndex := len(options) - 1 // (none) is always last
	model.entityForm.pickerCursor = noneIndex

	got, _ := model.updateTemplateDefaultPicker(tea.KeyMsg{Type: tea.KeyEnter})
	updated := got.(Model)

	bundle, err := updated.repos.Editor.Load()
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if got := bundle.TemplateByDefault("task", ""); got != nil {
		t.Fatalf("expected no template bound to task after (none), got %+v", got)
	}
}

func TestTemplateDefaultPickerOptionsIncludeProjectScope(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	options := buildTemplateDefaultOptions(model.repos.Editor, "my-project")
	hasProjectScoped := false
	for _, opt := range options {
		if opt.Kind == "task" && opt.ProjectSlug == "my-project" {
			hasProjectScoped = true
			break
		}
	}
	if !hasProjectScoped {
		t.Fatalf("expected per-project option for task in picker, got %+v", options)
	}
	// (none) row must always be present.
	if last := options[len(options)-1]; last.Kind != "" {
		t.Fatalf("last option should be (none), got %+v", last)
	}
}

// Sanity: build helper exercises live config so the picker resolves
// template_defaults from the loaded bundle, not a hardcoded list.
func TestTemplateDefaultPickerHonorsConfigTemplateDefaults(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	// Mutate the bundle to a tiny custom list.
	if _, err := model.repos.Editor.Apply(model.ctx, func(bundle *config.Bundle) error {
		bundle.Config.TemplateDefaults = []string{"task"}
		return nil
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	options := buildTemplateDefaultOptions(model.repos.Editor, "")
	for _, opt := range options {
		if opt.Kind == "pr" {
			t.Fatalf("config.template_defaults restricted to [task] but pr appeared: %+v", options)
		}
	}
}

package studio

import (
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestCommandPreviewCacheScopesSameCommandBySpecAndRuntime(t *testing.T) {
	t.Parallel()

	spec := config.MCPCommandSpec{Persona: "builder", Skills: []string{"code"}}
	changed := spec
	changed.Skills = []string{"review"}
	cache := &commandPreviewCache{}
	cache.projectID = 7
	cache.runtimeGeneration = 3
	cache.name = "okt-run"
	cache.spec = cloneCommandSpec(spec)
	cache.ready = true

	if !cache.matches(7, 3, "okt-run", spec) {
		t.Fatal("same-project, same-runtime command preview did not match")
	}
	if cache.matches(7, 3, "okt-run", changed) {
		t.Fatal("preview for a different command spec matched the cached result")
	}
	if cache.matches(8, 3, "okt-run", spec) {
		t.Fatal("preview crossed project scope")
	}
	if cache.matches(7, 4, "okt-run", spec) {
		t.Fatal("preview crossed runtime generation")
	}
}

func TestCommandPreviewDropsStaleSuccessAndFailureAfterRotation(t *testing.T) {
	t.Parallel()

	deps := studioBenchDeps(t)
	deps.ProjectID = 1
	deps.RuntimeGeneration = 1
	deps.ResolveCommand = func(config.Bundle, string) (string, error) { return "old preview", nil }
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(screenhost.StudioCommands, deps)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	cmd := screen.commandPreviewCmd()
	if cmd == nil {
		t.Fatal("initial preview command = nil")
	}
	staleSuccess := cmd().(commandPreviewMsg)

	deps.ProjectID = 2
	deps.RuntimeGeneration = 2
	rotated := screen.Bind(screenhost.StudioCommands, deps)
	for name, msg := range map[string]commandPreviewMsg{
		"success": staleSuccess,
		"failure": {projectID: 1, runtimeGeneration: 1, gen: staleSuccess.gen, name: staleSuccess.name, spec: staleSuccess.spec, err: errors.New("old failure")},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rotated
			candidate.applyCommandPreview(msg)
			if candidate.commandPreview.ready {
				t.Fatalf("stale %s completion populated the rotated preview cache", name)
			}
		})
	}
}

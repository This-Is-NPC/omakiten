package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/studio"
)

func TestStudioDirtyDraftCannotCrossProjectRoundTrip(t *testing.T) {
	t.Parallel()

	aPath, bPath := "/projects/a/.omakiten/config/a.yaml", "/projects/b/.omakiten/config/b.yaml"
	aStore := &studioDraftStore{bundle: studioDraftBundle()}
	bundleB := studioDraftBundle()
	bundleB.Workflows[0].Buckets[0].Name = "Project B"
	bStore := &studioDraftStore{bundle: bundleB}
	aEditor := bundleeditor.New(aStore, aPath)
	bEditor := bundleeditor.New(bStore, bPath)

	cache := agentruntime.NewBundleCache(nil, nil, nil)
	if err := cache.Install(1, &agentruntime.ProjectRuntime{Snapshot: config.BuildSnapshot(aStore.bundle), Editor: aEditor, SourcePath: aPath}); err != nil {
		t.Fatalf("install A runtime: %v", err)
	}
	if err := cache.Install(2, &agentruntime.ProjectRuntime{Snapshot: config.BuildSnapshot(bStore.bundle), Editor: bEditor, SourcePath: bPath}); err != nil {
		t.Fatalf("install B runtime: %v", err)
	}

	aDraft, err := NewStudioDraft(aEditor)
	if err != nil {
		t.Fatalf("open A draft: %v", err)
	}
	aDraft.RenameBucket(1, "A dirty candidate")
	m := Model{
		ctx:     context.Background(),
		project: domain.ProjectContext{ID: 1, Slug: "a"},
		top:     topHome,
		repos: Repositories{
			Cache:      cache,
			ProjectID:  1,
			Editor:     aEditor,
			ConfigPath: aPath,
		},
		studioScreen: studio.New().WithState(studio.State{
			Draft:             aDraft,
			ApplyOpen:         true,
			ApplyArmed:        true,
			ApplyConfirmation: "A candidate",
		}),
	}

	if err := m.selectHomeProject(domain.Project{ID: 2, Name: "B", Slug: "b", RootPath: "/projects/b"}); err != nil {
		t.Fatalf("select B: %v", err)
	}
	assertStudioSessionEmpty(t, m, "A to B")
	if m.repos.Editor != bEditor || m.repos.ConfigPath != bPath {
		t.Fatalf("B runtime binding = editor %p, path %q; want editor %p, path %q", m.repos.Editor, m.repos.ConfigPath, bEditor, bPath)
	}
	boundB := m.boundStudioScreen(screenhost.StudioCommands)
	outcome := boundB.Update(m.screenFrame(), tea.KeyMsg{Type: tea.KeyCtrlS})
	next, ok := outcome.Screen.(studio.Screen)
	if !ok || next.State().Draft == nil || next.State().Draft.Report().Candidate.Workflows[0].Buckets[0].Name != "Project B" {
		t.Fatal("B Studio did not bind B's current draft before apply")
	}
	if bStore.saves != 0 {
		t.Fatalf("B apply guard wrote the old A candidate, saves=%d", bStore.saves)
	}

	if err := m.selectHomeProject(domain.Project{ID: 1, Name: "A", Slug: "a", RootPath: "/projects/a"}); err != nil {
		t.Fatalf("select A again: %v", err)
	}
	assertStudioSessionEmpty(t, m, "B to A")

	// Returning to A is clean until Studio explicitly opens the current A
	// source again; the dirty object from the first visit is never reused.
	bound := m.boundStudioScreen(screenhost.StudioWorkflow)
	if !bound.StudioDraftOpen() {
		t.Fatal("re-entering A did not load its current source")
	}
	if got := bound.State().Draft.Report().Candidate.Workflows[0].Buckets[0].Name; got != "Backlog" {
		t.Fatalf("reloaded A candidate name = %q, want current source Backlog", got)
	}
}

func assertStudioSessionEmpty(t *testing.T, m Model, visit string) {
	t.Helper()
	state := m.studioScreen.State()
	if state.Draft != nil || state.ApplyOpen || state.ApplyArmed || state.ApplyConfirmation != "" {
		t.Fatalf("Studio state after %s retained candidate or apply state: %+v", visit, state)
	}
}

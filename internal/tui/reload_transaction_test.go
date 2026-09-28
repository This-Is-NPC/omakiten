package tui

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testfixtures/snapstore"
)

func TestRuntimeApplyFailureLeavesCacheAndModelUntouchedAtThemeStage(t *testing.T) {
	t.Parallel()
	m, _, _, _ := newStudioApplyModel(t)
	previous := m.repos.Cache.View(m.project.ID)
	m.studioApplyDiff = []string{"candidate diff"}
	m.studioOutcomeGeneration = 41
	m.studioOutcomeGenerationSet = true
	before := m

	bundle := tuiTestBundle(t)
	bundle.ActiveThemeErr = errors.New("theme stage failed")
	failed := &contract.RuntimeView{
		Snapshot:   config.BuildSnapshot(bundle),
		Editor:     previous.Editor,
		SourcePath: m.repos.ConfigPath,
	}
	staged := before
	if err := staged.applyProjectRuntime(failed, m.repos.ConfigPath); err == nil {
		t.Fatal("theme-stage apply succeeded")
	}
	assertRuntimeUntouched(t, m, previous, before)
}

func TestRuntimeApplyFailureLeavesStateUntouchedThenSameSourceRetrySucceeds(t *testing.T) {
	m, _, store, _ := newStudioApplyModel(t)
	previous := m.repos.Cache.View(m.project.ID)
	previousEditorPath := m.repos.Editor.Path()
	before := m
	bundle := tuiTestBundle(t)
	candidate := config.BuildSnapshot(bundle)
	failing := operation.NewService(failingTaskRepo{Store: store}, contract.ProjectSelector{ProjectID: m.project.ID})
	failing.SetSnapshot(candidate)
	failed := &contract.RuntimeView{Snapshot: candidate, Service: failing, Editor: previous.Editor, SourcePath: m.repos.ConfigPath}
	staged := before
	if err := staged.applyProjectRuntime(failed, m.repos.ConfigPath+".candidate"); err == nil {
		t.Fatal("refresh-stage apply succeeded")
	}
	assertRuntimeUntouched(t, m, previous, before)
	if got := m.repos.Editor.Path(); got != previousEditorPath {
		t.Fatalf("failed apply changed editor path to %q, want %q", got, previousEditorPath)
	}

	if err := m.reloadBundle(m.repos.ConfigPath); err != nil {
		t.Fatalf("same-source retry: %v", err)
	}
	current := m.repos.Cache.View(m.project.ID)
	if current == nil || current == previous {
		t.Fatal("same-source retry did not publish a fresh runtime")
	}
	if m.repos.activeSnapshot() != current.Snapshot || m.registry != current.EnumRegistry {
		t.Fatal("same-source retry left Model and cache on different runtime fields")
	}
}

func assertRuntimeUntouched(t *testing.T, m Model, previous *contract.RuntimeView, before Model) {
	t.Helper()
	if m.repos.Cache.View(m.project.ID) != previous {
		t.Fatal("failed apply left cache on the replacement runtime")
	}
	if m.repos.activeSnapshot() != previous.Snapshot || m.registry != before.registry || m.repos.Editor != before.repos.Editor {
		t.Fatal("failed apply left Model/cache on different prior runtime fields")
	}
	if m.studioRuntimeGeneration != before.studioRuntimeGeneration || !reflect.DeepEqual(m.studioApplyDiff, before.studioApplyDiff) {
		t.Fatal("failed staged apply changed live Studio metadata")
	}
	if m.studioOutcomeGenerationSet != before.studioOutcomeGenerationSet || m.studioOutcomeGeneration != before.studioOutcomeGeneration {
		t.Fatal("failed staged apply changed live Studio outcome metadata")
	}
}

type failingTaskRepo struct {
	*snapstore.Store
}

func (f failingTaskRepo) ListTasks(context.Context, int64, domain.TaskFilter, domain.BucketResolver) ([]domain.Task, error) {
	return nil, errors.New("task snapshot stage failed")
}

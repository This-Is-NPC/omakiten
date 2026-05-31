package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/events"
	"omakiten/internal/testfixtures/snapstore"
)

// TestBundleCacheRebuildsOnImportedFileMtimeChange pins acceptance #6 for the
// import feature: a profile that pulls config.hooks in via a `from:` import is
// rebuilt when the IMPORTED file's mtime changes — not just the root profile.
// The resolver records every imported file in Bundle.SourcePaths, the runtime
// stats all of them, and the cache rebuilds on any change. The proof is
// twofold: the cache pointer rotates, and the rebuilt snapshot reflects the
// edit made to the imported file (a hook that was absent before the change).
func TestBundleCacheRebuildsOnImportedFileMtimeChange(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	if err := config.EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}

	// Externalize config.hooks into an imported file with a single hook.
	importPath := pointConfigHooksAtImport(t, configPath, "hooks.yml",
		"- on: task.created\n  do: noop\n")

	store := snapstore.Open(t, filepath.Join(tmp, "omakiten.db"))
	files := configstore.New()
	bus := events.NewInProcessBus(config.EventsSettings{})
	cache := NewBundleCache(store.Store, bus, files)

	first, err := cache.Resolve(ctx, 0, configPath)
	if err != nil {
		t.Fatalf("Resolve initial: %v", err)
	}
	if first.Snapshot == nil || len(first.Snapshot.Hooks()) != 1 {
		t.Fatalf("initial Snapshot.Hooks() = %+v, want exactly the one imported hook", snapshotHooks(first))
	}
	// The imported file must be among the watched source paths — otherwise a
	// change to it could never trigger a rebuild.
	if !containsPath(first.SourcePaths, importPath) {
		t.Fatalf("SourcePaths %v does not track imported file %q", first.SourcePaths, importPath)
	}

	// Edit only the imported file (add a second hook) and bump its mtime; the
	// root profile is untouched. Resolve must still detect the change through
	// the tracked import source and rebuild.
	if err := os.WriteFile(importPath,
		[]byte("- on: task.created\n  do: noop\n- on: task.completed\n  do: noop\n"), 0o644); err != nil {
		t.Fatalf("rewrite import: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(importPath, future, future); err != nil {
		t.Fatalf("Chtimes(import): %v", err)
	}

	second, err := cache.Resolve(ctx, 0, configPath)
	if err != nil {
		t.Fatalf("Resolve after import mtime bump: %v", err)
	}
	if second == first {
		t.Fatal("cache did not rebuild after imported-file mtime change")
	}
	if got := len(snapshotHooks(second)); got != 2 {
		t.Fatalf("rebuilt Snapshot.Hooks() len = %d, want 2 (edit to imported file not picked up)", got)
	}
}

func snapshotHooks(rt *ProjectRuntime) []config.HookSpec {
	if rt == nil || rt.Snapshot == nil {
		return nil
	}
	return rt.Snapshot.Hooks()
}

func containsPath(paths []string, want string) bool {
	wantCanon := want
	if real, err := filepath.EvalSymlinks(want); err == nil {
		wantCanon = real
	}
	for _, p := range paths {
		if p == want || p == wantCanon {
			return true
		}
		if real, err := filepath.EvalSymlinks(p); err == nil && real == wantCanon {
			return true
		}
	}
	return false
}

// pointConfigHooksAtimport rewrites the profile at configPath so config.hooks
// becomes `{from: ./<importName>}` and writes importBody to that file next to
// the profile. Returns the absolute import path. Mirrors the config package's
// test rewrite but is reproduced here because that helper is unexported.
func pointConfigHooksAtImport(t *testing.T, configPath, importName, importBody string) string {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", configPath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal(%s): %v", configPath, err)
	}
	top := doc.Content[0]
	var cfg *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "config" {
			cfg = top.Content[i+1]
			break
		}
	}
	if cfg == nil || cfg.Kind != yaml.MappingNode {
		t.Fatal("profile has no config mapping")
	}
	directive := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "from"},
			{Kind: yaml.ScalarNode, Value: "./" + importName},
		},
	}
	replaced := false
	for i := 0; i+1 < len(cfg.Content); i += 2 {
		if cfg.Content[i].Value == "hooks" {
			cfg.Content[i+1] = directive
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Content = append(cfg.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "hooks"},
			directive,
		)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatalf("Marshal profile: %v", err)
	}
	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", configPath, err)
	}

	importPath := filepath.Join(filepath.Dir(configPath), importName)
	if err := os.WriteFile(importPath, []byte(importBody), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", importPath, err)
	}
	return importPath
}

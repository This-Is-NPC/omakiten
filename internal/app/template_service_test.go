package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/sqlite"
	"omakiten/internal/testfixtures"
)

// TestTemplateServiceSetDefaultRewritesFrontmatter exercises the frontmatter
// rewrite path: when the focused template is bound for a kind+project, the
// default key lands in its file and any sibling that previously held the
// same binding is cleared. End-to-end through the BundleEditor + sqlite
// import so the wiring round-trip is covered too.
func TestTemplateServiceSetDefaultRewritesFrontmatter(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(config) = %v", err)
	}
	configPath := filepath.Join(configDir, "omakiten.yaml")

	templatesDir := filepath.Join(tmp, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(templates) = %v", err)
	}
	// Two task templates: alpha (currently bound for project demo) and beta
	// (no binding yet). After SetDefault on beta, alpha must lose the binding.
	alphaPath := filepath.Join(templatesDir, "alpha.md")
	betaPath := filepath.Join(templatesDir, "beta.md")
	if err := os.WriteFile(alphaPath, []byte("---\nname: Alpha\nentity: task\ndefault: task\nproject: demo\n---\n\nbody alpha\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(alpha) = %v", err)
	}
	if err := os.WriteFile(betaPath, []byte("---\nname: Beta\nentity: task\n---\n\nbody beta\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(beta) = %v", err)
	}

	bundle, _ := testfixtures.LoadBundle(t, "with_project.yaml")
	cs := configstore.New()
	if err := cs.SaveBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveBundle = %v", err)
	}

	store, err := sqlite.Open(ctx, filepath.Join(tmp, "data.db"))
	if err != nil {
		t.Fatalf("sqlite.Open = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	editor := app.NewBundleEditor(cs, configPath)
	// Seed-import so subsequent ApplyWithFiles round-trips succeed.
	if _, err := editor.Apply(ctx, nil); err != nil {
		t.Fatalf("editor.Apply(seed) = %v", err)
	}

	svc := app.NewTemplateService(config.BuildSnapshot(bundle), editor, cs)
	if err := svc.SetDefault(ctx, "beta", "task", "demo"); err != nil {
		t.Fatalf("SetDefault(beta) = %v", err)
	}

	betaBytes, err := os.ReadFile(betaPath)
	if err != nil {
		t.Fatalf("ReadFile(beta) = %v", err)
	}
	if !strings.Contains(string(betaBytes), "default: task") {
		t.Fatalf("beta frontmatter missing default: task — got %q", betaBytes)
	}
	if !strings.Contains(string(betaBytes), "project: demo") {
		t.Fatalf("beta frontmatter missing project: demo — got %q", betaBytes)
	}

	alphaBytes, err := os.ReadFile(alphaPath)
	if err != nil {
		t.Fatalf("ReadFile(alpha) = %v", err)
	}
	if strings.Contains(string(alphaBytes), "default: task") {
		t.Fatalf("alpha still carries default binding after sibling claim — got %q", alphaBytes)
	}
}

// TestTemplateServiceSetDefaultClears verifies the kind=="" branch removes
// both the default and project keys from the focused template.
func TestTemplateServiceSetDefaultClears(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(config) = %v", err)
	}
	configPath := filepath.Join(configDir, "omakiten.yaml")
	templatesDir := filepath.Join(tmp, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(templates) = %v", err)
	}
	tplPath := filepath.Join(templatesDir, "only.md")
	if err := os.WriteFile(tplPath, []byte("---\nname: Only\nentity: task\ndefault: task\nproject: demo\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(only) = %v", err)
	}

	bundle, _ := testfixtures.LoadBundle(t, "with_project.yaml")
	cs := configstore.New()
	if err := cs.SaveBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveBundle = %v", err)
	}

	store, err := sqlite.Open(ctx, filepath.Join(tmp, "data.db"))
	if err != nil {
		t.Fatalf("sqlite.Open = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	editor := app.NewBundleEditor(cs, configPath)
	if _, err := editor.Apply(ctx, nil); err != nil {
		t.Fatalf("editor.Apply(seed) = %v", err)
	}

	svc := app.NewTemplateService(config.BuildSnapshot(bundle), editor, cs)
	if err := svc.SetDefault(ctx, "only", "", ""); err != nil {
		t.Fatalf("SetDefault(clear) = %v", err)
	}

	got, err := os.ReadFile(tplPath)
	if err != nil {
		t.Fatalf("ReadFile(only) = %v", err)
	}
	if strings.Contains(string(got), "default:") {
		t.Fatalf("template still has default key after clear — got %q", got)
	}
	if strings.Contains(string(got), "project:") {
		t.Fatalf("template still has project key after clear — got %q", got)
	}
}

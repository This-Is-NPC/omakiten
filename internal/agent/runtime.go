package agent

import (
	"context"
	"os"
	"path/filepath"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/paths"
	"omakiten/internal/sqlite"
)

type Options struct {
	DBPath     string
	ConfigPath string
	Project    string
	ProjectID  int64
	CWD        string
}

type Runtime struct {
	store      *sqlite.Store
	configPath string
	dbPath     string
	service    *Service
}

func Open(ctx context.Context, opts Options) (*Runtime, error) {
	configPath, err := resolvedConfigPath(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	dbPath, err := resolvedDBPath(opts.DBPath)
	if err != nil {
		return nil, err
	}

	rootDir := config.ConfigRootFromYAMLPath(configPath)
	if err := config.MigrateLayout(rootDir); err != nil {
		return nil, err
	}
	if err := config.EnsureDefaultFiles(rootDir); err != nil {
		return nil, err
	}

	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	bundle, _, err := app.NewConfigService(store).Import(ctx, configPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	cwd := opts.CWD
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			_ = store.Close()
			return nil, err
		}
	}

	runtime := &Runtime{store: store, configPath: configPath, dbPath: dbPath}
	runtime.service = NewService(store, ProjectSelector{ProjectID: opts.ProjectID, Project: opts.Project, CWD: cwd})
	runtime.service.SetTaskTemplateLookup(taskTemplateLookup(bundle))
	runtime.service.SetTemplateCatalog(templateCatalog(bundle))
	return runtime, nil
}

// templateCatalog snapshots the bundle's templates into the read-only view
// the MCP catalog endpoints expose. Snapshot at startup is enough — the
// agent is read-only and would only see drift after a config refresh, which
// requires restarting the runtime anyway.
func templateCatalog(bundle config.Bundle) TemplateCatalog {
	snapshot := make([]TemplateSummary, 0, len(bundle.Templates))
	for _, t := range bundle.Templates {
		snapshot = append(snapshot, TemplateSummary{
			Slug:        t.Slug,
			Name:        t.Name,
			Description: t.Description,
			Entity:      t.Entity,
			Default:     t.Default,
			Project:     t.ProjectSlug,
			IsCustom:    t.IsCustom,
			Body:        t.Body,
			SourcePath:  t.SourcePath,
		})
	}
	return func() []TemplateSummary {
		out := make([]TemplateSummary, len(snapshot))
		copy(out, snapshot)
		return out
	}
}

// taskTemplateLookup captures the bundle at runtime startup and returns a
// project-aware closure that resolves the active task template scaffold on
// demand. Project-scoped templates win over global; nil means no template
// is configured for the kind.
func taskTemplateLookup(bundle config.Bundle) TaskTemplateLookup {
	templates := append([]config.TaskTemplate(nil), bundle.Templates...)
	return func(projectSlug string) *TaskTemplateSummary {
		var global *config.TaskTemplate
		for i := range templates {
			t := &templates[i]
			if t.Default != "task" {
				continue
			}
			if projectSlug != "" && t.ProjectSlug == projectSlug {
				return summarizeTaskTemplate(t)
			}
			if t.ProjectSlug == "" && global == nil {
				global = t
			}
		}
		if global == nil {
			return nil
		}
		return summarizeTaskTemplate(global)
	}
}

func summarizeTaskTemplate(t *config.TaskTemplate) *TaskTemplateSummary {
	if t == nil {
		return nil
	}
	return &TaskTemplateSummary{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Body:        t.Body,
	}
}

func (r *Runtime) Close() error {
	return r.store.Close()
}

func (r *Runtime) Service() *Service {
	return r.service
}

func (r *Runtime) Store() *sqlite.Store {
	return r.store
}

func (r *Runtime) ConfigPath() string {
	return r.configPath
}

func (r *Runtime) DBPath() string {
	return r.dbPath
}

func resolvedConfigPath(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return paths.ConfigFile()
}

func resolvedDBPath(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return paths.DatabaseFile()
}

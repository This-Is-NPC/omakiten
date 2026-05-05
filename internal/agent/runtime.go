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
	return runtime, nil
}

// taskTemplateLookup captures the bundle at runtime startup and returns a
// closure that resolves the active task template scaffold on demand. Returns
// nil when no template is configured or the slug points at a missing file.
func taskTemplateLookup(bundle config.Bundle) TaskTemplateLookup {
	slug := bundle.Config.Templates.Task
	if slug == "" {
		return nil
	}
	for _, tpl := range bundle.Templates {
		if tpl.Slug == slug {
			summary := TaskTemplateSummary{
				Slug:        tpl.Slug,
				Name:        tpl.Name,
				Description: tpl.Description,
				Body:        tpl.Body,
			}
			return func() *TaskTemplateSummary {
				out := summary
				return &out
			}
		}
	}
	return nil
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

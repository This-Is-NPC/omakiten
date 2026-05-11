package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/agentsetup"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func newInitCommand(opts *runtimeOptions) *cobra.Command {
	var name string
	var slug string
	var root string
	var enableMCP bool
	var mcpHarness string
	var mcpConfigPath string
	var mcpCommand string
	var mcpDryRun bool
	var mcpForce bool
	var presetName string
	var presetForce bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the current project in the global database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				projectRoot := root
				if projectRoot == "" {
					var err error
					projectRoot, err = os.Getwd()
					if err != nil {
						return nil, err
					}
				}

				var presetResult map[string]any
				if presetName != "" {
					installRoot, err := presetInstallRoot(projectRoot, root != "")
					if err != nil {
						return nil, err
					}
					if root == "" {
						projectRoot = installRoot
					}
					preset, path, err := config.CopyPreset(presetName, filepath.Join(installRoot, ".omakiten"), presetForce)
					if err != nil {
						return nil, presetCLIError(err)
					}
					presetResult = map[string]any{"name": preset.Name, "title": preset.Title, "path": path, "root": installRoot}
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := app.NewProjectService(rt.store).Init(ctx, name, slug, projectRoot)
				if err != nil {
					return nil, err
				}

				result := map[string]any{"project": project, "db_path": rt.dbPath, "config_path": rt.configPath}
				if presetResult != nil {
					result["preset"] = presetResult
				}
				if enableMCP {
					setup, err := agentsetup.Setup(agentsetup.Options{Harness: mcpHarness, ConfigPath: mcpConfigPath, Command: mcpCommand, DryRun: mcpDryRun, Force: mcpForce})
					if err != nil {
						return nil, err
					}
					result["agent_setup"] = setup
				}
				return result, nil
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "project name")
	cmd.Flags().StringVar(&slug, "slug", "", "project slug")
	cmd.Flags().StringVar(&root, "root", "", "project root path")
	cmd.Flags().BoolVar(&enableMCP, "enable-mcp", false, "enable global MCP agent access for a supported harness")
	cmd.Flags().StringVar(&mcpHarness, "mcp-harness", agentsetup.ClaudeCodeHarness, "MCP harness to configure")
	cmd.Flags().StringVar(&mcpConfigPath, "mcp-config", "", "MCP harness config path")
	cmd.Flags().StringVar(&mcpCommand, "mcp-command", "", "command path written to the harness MCP config")
	cmd.Flags().BoolVar(&mcpDryRun, "mcp-dry-run", false, "preview MCP harness config changes without writing")
	cmd.Flags().BoolVar(&mcpForce, "mcp-force", false, "replace an existing Omakiten MCP harness entry")
	cmd.Flags().StringVar(&presetName, "preset", "", "official workflow preset to copy into .omakiten/config/omakiten.yaml")
	cmd.Flags().BoolVar(&presetForce, "preset-force", false, "overwrite an existing .omakiten preset config")
	return cmd
}

func presetCLIError(err error) error {
	if errors.Is(err, config.ErrPresetNotFound) {
		return domain.NewError(domain.ErrValidation, "unknown workflow preset", map[string]any{"available": config.ListPresets()})
	}
	if errors.Is(err, config.ErrPresetTargetExists) {
		return domain.NewError(domain.ErrValidation, "repo-local .omakiten already exists; pass --preset-force to overwrite", nil)
	}
	return err
}

func presetInstallRoot(start string, explicitRoot bool) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if explicitRoot {
		return abs, nil
	}
	return gitRootOrSelf(abs)
}

func gitRootOrSelf(start string) (string, error) {
	dir := start
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start, nil
		}
		dir = parent
	}
}

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
		Short: opts.t("cli.init.short"),
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
					repoLocalRoot := filepath.Join(installRoot, config.RepoLocalDirName)
					res, err := config.SeedInstall(repoLocalRoot, presetName, presetForce)
					if err != nil {
						return nil, presetCLIError(opts, err)
					}
					presetResult = map[string]any{"name": res.PresetName, "path": res.Path, "root": installRoot}
					if res.NoOp {
						presetResult["no_op"] = true
					}
					if res.Refreshed {
						presetResult["refreshed"] = true
					}
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

	cmd.Flags().StringVar(&name, "name", "", opts.t("cli.init.flag.name"))
	cmd.Flags().StringVar(&slug, "slug", "", opts.t("cli.init.flag.slug"))
	cmd.Flags().StringVar(&root, "root", "", opts.t("cli.init.flag.root"))
	cmd.Flags().BoolVar(&enableMCP, "enable-mcp", false, opts.t("cli.init.flag.enable-mcp"))
	cmd.Flags().StringVar(&mcpHarness, "mcp-harness", agentsetup.ClaudeCodeHarness, opts.t("cli.init.flag.mcp-harness"))
	cmd.Flags().StringVar(&mcpConfigPath, "mcp-config", "", opts.t("cli.init.flag.mcp-config"))
	cmd.Flags().StringVar(&mcpCommand, "mcp-command", "", opts.t("cli.init.flag.mcp-command"))
	cmd.Flags().BoolVar(&mcpDryRun, "mcp-dry-run", false, opts.t("cli.init.flag.mcp-dry-run"))
	cmd.Flags().BoolVar(&mcpForce, "mcp-force", false, opts.t("cli.init.flag.mcp-force"))
	cmd.Flags().StringVar(&presetName, "preset", "", opts.t("cli.init.flag.preset"))
	cmd.Flags().BoolVar(&presetForce, "preset-force", false, opts.t("cli.init.flag.preset-force"))
	return cmd
}

func presetCLIError(opts *runtimeOptions, err error) error {
	if errors.Is(err, config.ErrPresetNotFound) {
		return domain.NewError(domain.ErrValidation, t("cli.err.unknown_workflow_preset"), map[string]any{"available": resolvedPresets(opts)})
	}
	if errors.Is(err, config.ErrPresetTargetExists) {
		return domain.NewError(domain.ErrValidation, t("cli.err.repo_local_already_exists"), nil)
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

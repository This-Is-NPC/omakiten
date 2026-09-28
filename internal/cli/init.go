package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/agentruntime"
	"omakiten/internal/agentsetup"
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
				return runInit(ctx, opts, initInputs{
					name: name, slug: slug, root: root, enableMCP: enableMCP,
					mcpHarness: mcpHarness, mcpConfigPath: mcpConfigPath,
					mcpCommand: mcpCommand, mcpDryRun: mcpDryRun, mcpForce: mcpForce,
					presetName: presetName, presetForce: presetForce,
				})
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

type initInputs struct {
	name, slug, root string
	enableMCP        bool
	mcpHarness       string
	mcpConfigPath    string
	mcpCommand       string
	mcpDryRun        bool
	mcpForce         bool
	presetName       string
	presetForce      bool
}

func runInit(ctx context.Context, opts *runtimeOptions, inputs initInputs) (any, error) {
	projectRoot, err := initProjectRoot(inputs.root)
	if err != nil {
		return nil, err
	}
	projectRoot, presetResult, err := installInitPreset(projectRoot, inputs, opts)
	if err != nil {
		return nil, err
	}
	rt, err := opts.open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer rt.close()
	ctx = rt.WithActivityRepo(ctx)
	project, err := agentruntime.InitProject(ctx, rt.store, inputs.name, inputs.slug, projectRoot)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"project": project, "db_path": rt.dbPath, "config_path": rt.configPath}
	if presetResult != nil {
		result["preset"] = presetResult
	}
	if inputs.enableMCP {
		setup, err := agentsetup.Setup(agentsetup.Options{
			Harness: inputs.mcpHarness, ConfigPath: inputs.mcpConfigPath, ProjectRoot: projectRoot,
			Command: inputs.mcpCommand, DryRun: inputs.mcpDryRun, Force: inputs.mcpForce,
		})
		if err != nil {
			return nil, err
		}
		result["agent_setup"] = setup
	}
	return result, nil
}

func initProjectRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	return os.Getwd()
}

func installInitPreset(projectRoot string, inputs initInputs, opts *runtimeOptions) (string, map[string]any, error) {
	if inputs.presetName == "" {
		return projectRoot, nil, nil
	}
	installRoot, err := presetInstallRoot(projectRoot, inputs.root != "")
	if err != nil {
		return "", nil, err
	}
	if inputs.root == "" {
		projectRoot = installRoot
	}
	res, err := config.SeedInstall(filepath.Join(installRoot, config.RepoLocalDirName), inputs.presetName, inputs.presetForce)
	if err != nil {
		return "", nil, presetCLIError(opts, err)
	}
	presetResult := map[string]any{"name": res.PresetName, "path": res.Path, "root": installRoot}
	if res.NoOp {
		presetResult["no_op"] = true
	}
	if res.Refreshed {
		presetResult["refreshed"] = true
	}
	return projectRoot, presetResult, nil
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

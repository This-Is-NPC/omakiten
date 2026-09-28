package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
)

func newInitCommand(opts *runtimeOptions) *cobra.Command {
	var name string
	var slug string
	var root string
	var installSkill, claudeCode bool
	var presetName string
	var presetForce bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: opts.t("cli.init.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runInit(ctx, opts, initInputs{
					name: name, slug: slug, root: root,
					presetName: presetName, presetForce: presetForce, installSkill: installSkill, claudeCode: claudeCode,
				})
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", opts.t("cli.init.flag.name"))
	cmd.Flags().StringVar(&slug, "slug", "", opts.t("cli.init.flag.slug"))
	cmd.Flags().StringVar(&root, "root", "", opts.t("cli.init.flag.root"))
	cmd.Flags().BoolVar(&installSkill, "skill", false, opts.t("cli.init.flag.skill"))
	cmd.Flags().BoolVar(&claudeCode, "claude-code", false, opts.t("cli.init.flag.claude-code"))
	cmd.Flags().StringVar(&presetName, "preset", "", opts.t("cli.init.flag.preset"))
	cmd.Flags().BoolVar(&presetForce, "preset-force", false, opts.t("cli.init.flag.preset-force"))
	return cmd
}

type initInputs struct {
	name, slug, root         string
	installSkill, claudeCode bool
	presetName               string
	presetForce              bool
}

func runInit(ctx context.Context, opts *runtimeOptions, inputs initInputs) (any, error) {
	if inputs.claudeCode && !inputs.installSkill {
		return nil, domain.NewError(domain.ErrValidation, "--claude-code requires --skill", nil)
	}
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
	if inputs.installSkill {
		targets := []string{"agents"}
		if inputs.claudeCode {
			targets = append(targets, "claude-code")
		}
		skills, err := installer.InstallSkills(projectRoot, targets, inputs.presetForce)
		if err != nil {
			return nil, err
		}
		result["skills"] = skills
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

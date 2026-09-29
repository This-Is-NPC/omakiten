package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func newPresetCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "preset", Short: opts.t("cli.preset.short")}
	cmd.AddCommand(newPresetScopeCommand(opts, "add DIRECTORY", "add", func(_ *cobra.Command, root string, args []string) (any, error) {
		path, err := filepath.Abs(args[0])
		if err != nil {
			return nil, err
		}
		p, err := config.ReadPresetDirectory(path)
		if err != nil {
			return nil, err
		}
		if p.Manifest.Origin.Source == "" {
			p.Manifest.Origin.Source = path
		}
		return config.InstallPreset(root, p)
	}))
	cmd.AddCommand(newPresetScopeCommand(opts, "use NAME_OR_ID", "use", func(_ *cobra.Command, root string, args []string) (any, error) {
		return config.ActivatePreset(root, args[0])
	}))
	cmd.AddCommand(newPresetScopeCommand(opts, "list", "list", func(_ *cobra.Command, root string, _ []string) (any, error) {
		return config.InstalledPresets(root)
	}))
	cmd.AddCommand(newPresetImportCommand(opts), newPresetExportCommand(opts))
	return cmd
}

func newPresetScopeCommand(opts *runtimeOptions, use, action string, run func(*cobra.Command, string, []string) (any, error)) *cobra.Command {
	var scope string
	cmd := &cobra.Command{Use: use, Short: opts.t("cli.preset." + action + ".short"), Args: cobra.ExactArgs(1)}
	if action == "list" || action == "import" {
		cmd.Args = cobra.NoArgs
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runJSON(cmd, func(ctx context.Context) (any, error) {
			if err := primeDiscoveryStart(ctx, opts); err != nil {
				return nil, err
			}
			root, err := resolveScopeRoot(opts, scope)
			if err != nil {
				return nil, err
			}
			result, err := run(cmd, root, args)
			return result, presetPackageCLIError(err)
		})
	}
	cmd.Flags().StringVar(&scope, "scope", "local", opts.t("cli.config.init.flag.scope"))
	return cmd
}

func newPresetImportCommand(opts *runtimeOptions) *cobra.Command {
	var file string
	cmd := newPresetScopeCommand(opts, "import --file PATH", "import", func(cmd *cobra.Command, root string, _ []string) (any, error) {
		raw, err := readWorkFile(cmd, file)
		if err != nil {
			return nil, err
		}
		p, err := config.DecodePreset(raw)
		if err != nil {
			return nil, err
		}
		return config.InstallPreset(root, p)
	})
	cmd.Flags().StringVar(&file, "file", "", opts.t("cli.work.flag.file"))
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newPresetExportCommand(opts *runtimeOptions) *cobra.Command {
	var output, name, version string
	var force bool
	cmd := &cobra.Command{Use: "export", Short: opts.t("cli.preset.export.short"), Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runDocumentExport(cmd, output, func(ctx context.Context) (any, []byte, error) {
			if err := primeDiscoveryStart(ctx, opts); err != nil {
				return nil, nil, err
			}
			path, err := opts.resolvedConfigPath()
			if err != nil {
				return nil, nil, err
			}
			p, err := exportPresetPackage(path, name, version)
			if err != nil {
				return nil, nil, presetPackageCLIError(err)
			}
			raw, err := config.EncodePreset(p)
			if err == nil && output != "-" {
				err = writeWorkFile(output, raw, force)
			}
			return map[string]any{"path": output, "manifest": p.Manifest}, raw, presetPackageCLIError(err)
		})
	}
	cmd.Flags().StringVar(&output, "output", "-", opts.t("cli.work.flag.output"))
	cmd.Flags().BoolVar(&force, "force", false, opts.t("cli.work.flag.force"))
	cmd.Flags().StringVar(&name, "name", "", opts.t("cli.preset.flag.name"))
	cmd.Flags().StringVar(&version, "version", "", opts.t("cli.preset.flag.version"))
	return cmd
}

func exportPresetPackage(path, name, version string) (config.PresetPackage, error) {
	physical, selected, err := config.ResolvePresetSelection(path)
	if err != nil {
		return config.PresetPackage{}, err
	}
	if !selected {
		return config.SnapshotPreset(path, name, version)
	}
	p, err := config.ReadPresetDirectory(config.ConfigRootFromYAMLPath(physical))
	if name != "" {
		p.Manifest.Name = name
	}
	if version != "" {
		p.Manifest.Version = version
	}
	return p, err
}

func presetPackageCLIError(err error) error {
	if err == nil {
		return nil
	}
	return domain.NewError(domain.ErrValidation, fmt.Sprint(err), nil)
}

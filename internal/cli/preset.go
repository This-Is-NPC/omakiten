package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
)

func newPresetCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "preset", Short: opts.t("cli.preset.short")}
	cmd.AddCommand(newPresetScopeCommand(opts, "add SOURCE", "add", func(cmd *cobra.Command, root string, args []string) (any, error) {
		p, err := installer.ReadPreset(cmd.Context(), args[0])
		if err != nil {
			return nil, err
		}
		return config.InstallPreset(root, p)
	}))
	use := newPresetScopeCommand(opts, "use NAME_OR_ID", "use", func(cmd *cobra.Command, root string, args []string) (any, error) {
		confirmed, _ := cmd.Flags().GetBool("confirm")
		return config.ActivatePresetApproved(root, args[0], func(p config.PresetInstallation, executables []string) error {
			if len(executables) == 0 || confirmed {
				return nil
			}
			return domain.NewError(domain.ErrConfirmationRequired, opts.t("cli.preset.use.confirm_executables"), map[string]any{"preset": p.Manifest.Name, "executables": executables})
		})
	})
	use.Flags().Bool("confirm", false, opts.t("cli.preset.use.flag.confirm"))
	cmd.AddCommand(use)
	cmd.AddCommand(newPresetScopeCommand(opts, "list", "list", func(_ *cobra.Command, root string, _ []string) (any, error) {
		return config.InstalledPresets(root)
	}))
	cmd.AddCommand(newPresetImportCommand(opts), newPresetExportCommand(opts))
	cmd.AddCommand(&cobra.Command{Use: "catalog", Short: opts.t("cli.preset.catalog.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(context.Context) (any, error) {
			return map[string]any{"presets": resolvedPresets(opts)}, nil
		})
	}})
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
			if action == "add" || action == "import" {
				warnNewLocalScope(cmd, opts, root)
			}
			result, err := run(cmd, root, args)
			return result, presetPackageCLIError(err)
		})
	}
	cmd.Flags().StringVar(&scope, "scope", "local", opts.t("cli.config.init.flag.scope"))
	return cmd
}

// warnNewLocalScope tells the user on stderr that an install without
// --scope is about to create the local scope at root.
func warnNewLocalScope(cmd *cobra.Command, opts *runtimeOptions, root string) {
	if cmd.Flags().Changed("scope") {
		return
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		fmt.Fprintf(cmd.ErrOrStderr(), opts.t("cli.preset.local_scope_warn_fmt")+"\n", root)
	}
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
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		return err
	}
	return domain.NewError(domain.ErrValidation, fmt.Sprint(err), nil)
}

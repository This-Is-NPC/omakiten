package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
	"omakiten/internal/sqlite"
)

const updateDefaultsManualCommand = "okt config refresh-defaults"

func updateDefaultsManualCommandForConfig(configPath string) string {
	if configPath == "" {
		return updateDefaultsManualCommand
	}
	rootDir := config.ConfigRootFromYAMLPath(configPath)
	if err := config.ValidateDefaultRefreshRoot(rootDir, configPath); err != nil {
		return "okt setup"
	}
	return "okt --config " + shellQuoteArg(configPath) + " config refresh-defaults"
}

func shellQuoteArg(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}();<>|&") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// validationRepairCommand selects an explicit repair for the reported file.
func validationRepairCommand(path, kind string) string {
	switch kind {
	case "missing_shipped_file", "embedded_default_drift":
		return updateDefaultsManualCommandForConfig(path)
	default:
		return "${EDITOR:-vi} " + shellQuoteArg(path)
	}
}

func isManagedConfigProfile(path string) bool {
	profile, err := validateManagedConfigProfilePath(path)
	if err != nil {
		return false
	}
	return config.ValidateDefaultRefreshRoot(config.ConfigRootFromYAMLPath(profile), profile) == nil
}

// classifyValidationError maps a single LoadBundle / ValidateBundle
// error string to a remediation-catalogue kind. ValidateBundle returns
// one error per call today; when that signature grows a structured
// slice (#368 follow-up), the classifier is the only file that needs
// to change — the envelope shape stays stable.
//
// Matching is intentionally permissive (substring, lower-case) so a
// validator copy refresh does not silently demote every error to
// `validation`. The default branch keeps the catalogue exhaustive: any
// classifier output selects a concrete repair for the reported path.
func classifyValidationError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	// Ordering matters: `is required` / `must be set` must run
	// before the `theme` substring branch so the validator string
	// `config.theme.active is required` resolves to
	// missing_required_key, not theme_not_found. The theme branch
	// requires the dedicated `active theme` phrasing the loader
	// emits when a theme slug is unknown (resolveActiveTheme).
	switch {
	case strings.Contains(msg, "is required") || strings.Contains(msg, "must be set"):
		return "missing_required_key"
	case strings.Contains(msg, "active theme"):
		return "theme_not_found"
	case (strings.Contains(msg, "unknown") && (strings.Contains(msg, "key") || strings.Contains(msg, "field"))) || strings.Contains(msg, "not found in type"):
		return "unknown_schema_key"
	case strings.Contains(msg, "must be") || strings.Contains(msg, "cannot be") || strings.Contains(msg, "between"):
		return "invalid_value"
	case strings.Contains(msg, "no such file") || strings.Contains(msg, "does not exist"):
		return "missing_shipped_file"
	default:
		return "validation"
	}
}

// buildValidateFailureDetails packages a single validator error into
// the `details` map the failure envelope ships. The shape pins
// #365 AC 1 plus #367's i18n hint: `{errors: [{kind, path, message,
// suggested_command, hint}], warnings: [...]}`. Callers compose this
// around the outer domain.NewError so `runJSON` writes an `ok:
// false` envelope with the structured payload nested under
// `details`.
//
// `hint` is the i18n-resolved prose copy from
// cli.config.validate.remediation.<kind>; the format-string slot
// receives the catalogued command literal so a future locale
// override of the prose still names the same command — translators
// cannot invent new ones (`law: no-assumptions`).
func buildValidateFailureDetails(path string, err error, warnings []string) map[string]any {
	kind := classifyValidationError(err)
	repairPath := path
	var source *config.SourceError
	if errors.As(err, &source) {
		repairPath = source.Path
	}
	remediation := kind
	if kind == "unknown_schema_key" && source == nil && isManagedConfigProfile(path) {
		remediation = "missing_shipped_file"
	}
	command := validationRepairCommand(repairPath, remediation)
	hint := fmt.Sprintf(t("cli.config.validate.remediation."+remediation), command)
	return map[string]any{
		"path": path,
		"errors": []map[string]any{
			{
				"kind":              kind,
				"path":              repairPath,
				"message":           domain.SafeError(err),
				"suggested_command": command,
				"hint":              hint,
			},
		},
		"warnings": warnings,
	}
}

// extractBundleWarnings flattens bundle.Warnings to the string slice
// the envelope ships under `warnings`. Each warning becomes a
// "<path>: <message>" line when a path is present so the JSON consumer
// can render the source without re-implementing the SourceWarning
// shape.
func extractBundleWarnings(bundle config.Bundle) []string {
	out := make([]string, 0, len(bundle.Warnings))
	for _, w := range bundle.Warnings {
		if strings.TrimSpace(w.Path) != "" {
			out = append(out, w.Path+": "+w.Message)
			continue
		}
		out = append(out, w.Message)
	}
	return out
}

func newConfigCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: opts.t("cli.config.short"),
	}
	cmd.AddCommand(newConfigValidateCommand(opts))
	cmd.AddCommand(newConfigRefreshDefaultsCommand(opts))
	cmd.AddCommand(newConfigSurfacesCommand(opts))
	cmd.AddCommand(newConfigInitCommand(opts))
	cmd.AddCommand(newConfigShowCommand(opts))
	cmd.AddCommand(newConfigPathCommand(opts))
	cmd.AddCommand(newConfigWhyCommand(opts))
	cmd.AddCommand(newConfigDiffCommand(opts))
	cmd.AddCommand(newConfigLanguageCommand(opts))
	return cmd
}

func newConfigValidateCommand(opts *runtimeOptions) *cobra.Command {
	var migrate bool
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: opts.t("cli.config.validate.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigValidate(cmd, opts, args, migrate)
		},
	}
	cmd.Flags().BoolVar(&migrate, "migrate", false, opts.t("cli.config.validate.flag.migrate"))
	_ = cmd.Flags().MarkHidden("migrate")
	return cmd
}

func runConfigValidate(cmd *cobra.Command, opts *runtimeOptions, args []string, migrate bool) error {
	return runJSON(cmd, func(ctx context.Context) (any, error) {
		if len(args) == 0 && opts.configPath == "" {
			if err := primeDiscoveryStart(ctx, opts); err != nil {
				return nil, err
			}
		}
		path, err := validationConfigPath(opts, args)
		if err != nil {
			return nil, err
		}
		if migrate {
			return runV030TransitionValidation(ctx, opts, path)
		}
		bundle, err := loadConfigForValidation(path)
		if err != nil {
			return nil, err
		}
		if bundle.ActiveThemeErr != nil {
			return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.theme_invalid"), buildValidateFailureDetails(path, bundle.ActiveThemeErr, extractBundleWarnings(bundle)))
		}
		return map[string]any{"path": path, "kit": bundle.Kit}, nil
	})
}

func runV030TransitionValidation(ctx context.Context, opts *runtimeOptions, path string) (any, error) {
	dbPath, err := opts.resolvedDBPath()
	if err != nil {
		return nil, err
	}
	if err := sqlite.ValidateV030ReleaseDatabase(ctx, dbPath); err != nil {
		return nil, configValidationFailure(path, err)
	}
	configPath, err := validateManagedConfigProfilePath(path)
	if err != nil {
		return nil, configValidationFailure(path, err)
	}
	return map[string]any{
		"path":       configPath,
		"db_path":    dbPath,
		"transition": "v0.30.0-to-current",
		"errors":     []map[string]any{},
		"warnings":   []string{},
	}, nil
}

func validateManagedConfigProfilePath(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("config path is invalid: %w", err)
	}
	rootDir := config.ConfigRootFromYAMLPath(absolutePath)
	configDir := filepath.Join(rootDir, "config")
	if filepath.Dir(absolutePath) != configDir {
		return "", fmt.Errorf("config requires an official managed preset under %s", configDir)
	}
	base := filepath.Base(absolutePath)
	if filepath.Ext(base) != ".yaml" {
		return "", fmt.Errorf("config requires an official managed preset under %s", configDir)
	}
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if _, ok := config.PresetByName(name); !ok || base != name+".yaml" {
		return "", fmt.Errorf("config requires one of the official managed presets: omakase.yaml, izakaya.yaml, kaiseki.yaml, shokunin.yaml")
	}
	return absolutePath, nil
}

func validationConfigPath(opts *runtimeOptions, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	return opts.resolvedConfigPath()
}

func loadConfigForValidation(path string) (config.Bundle, error) {
	bundle, err := config.LoadBundle(path)
	if err == nil {
		return bundle, nil
	}
	return config.Bundle{}, configValidationFailure(path, err)
}

func configValidationFailure(path string, err error) error {
	return domain.NewError(domain.ErrConfigInvalid, t("cli.err.config_invalid"), buildValidateFailureDetails(path, err, nil))
}

func newConfigRefreshDefaultsCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "refresh-defaults",
		Short:   opts.t("cli.config.refresh_defaults.short"),
		Args:    cobra.NoArgs,
		Example: "  okt config refresh-defaults\n  okt --config /path/to/.omakiten/config/omakase.yaml config refresh-defaults",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigRefreshDefaults(cmd, opts)
		},
	}
}

func runConfigRefreshDefaults(cmd *cobra.Command, opts *runtimeOptions) error {
	return runJSON(cmd, func(ctx context.Context) (any, error) {
		if opts.configPath == "" {
			if err := primeDiscoveryStart(ctx, opts); err != nil {
				return nil, err
			}
		}
		root, err := opts.resolvedConfigRoot()
		if err != nil {
			return nil, err
		}
		configPath := ""
		if resolved, err := opts.resolvedConfigPath(); err == nil {
			configPath = resolved
		} else if opts.configPath != "" || !os.IsNotExist(err) {
			// Only a missing implicit profile permits refreshing the default root.
			return nil, err
		}
		if err := config.ValidateDefaultRefreshRoot(root, configPath); err != nil {
			return nil, domain.NewError(domain.ErrValidation, "refusing to refresh defaults: "+err.Error(), map[string]any{
				"root":        root,
				"config_path": configPath,
			})
		}
		if err := config.RefreshDefaultFiles(root); err != nil {
			// A failed refresh can leave partial writes; its repair is idempotent.
			return nil, domain.NewError(domain.ErrUpdateFailed, "defaults refresh failed mid-write (a partial overwrite may have applied); re-run the repair command to finish: "+err.Error(), map[string]any{
				"root":           root,
				"config_path":    configPath,
				"partial_write":  true,
				"repair_command": updateDefaultsManualCommandForConfig(configPath),
				"error":          domain.SafeError(err),
			})
		}
		return refreshIntegrationSkills(root, configPath)
	})
}

func refreshIntegrationSkills(root, configPath string) (map[string]any, error) {
	skillRoot := ""
	if filepath.Base(root) == config.RepoLocalDirName {
		skillRoot = filepath.Dir(root)
	}
	skills, err := installer.RefreshSkills(skillRoot)
	if err != nil {
		return nil, domain.NewError(domain.ErrUpdateFailed, "integration skill refresh failed: "+err.Error(), map[string]any{"root": root, "repair_command": updateDefaultsManualCommandForConfig(configPath)})
	}
	return map[string]any{"root": root, "refreshed": true, "skills": skills}, nil
}

func newConfigSurfacesCommand(opts *runtimeOptions) *cobra.Command {
	var scaffold, check bool
	cmd := &cobra.Command{
		Use:   "surfaces",
		Short: opts.t("cli.config.surfaces.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case scaffold:
				_, err := fmt.Fprint(cmd.OutOrStdout(), config.SurfaceScaffoldYAML())
				return err
			case check:
				return runJSON(cmd, func(context.Context) (any, error) {
					return runConfigSurfacesCheck(opts)
				})
			default:
				return fmt.Errorf("exactly one of --scaffold or --check is required")
			}
		},
	}
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, opts.t("cli.config.surfaces.flag.scaffold"))
	cmd.Flags().BoolVar(&check, "check", false, opts.t("cli.config.surfaces.flag.check"))
	cmd.MarkFlagsMutuallyExclusive("scaffold", "check")
	cmd.MarkFlagsOneRequired("scaffold", "check")
	return cmd
}

func runConfigSurfacesCheck(opts *runtimeOptions) (any, error) {
	path, err := opts.resolvedConfigPath()
	if err != nil {
		return nil, err
	}
	table, err := config.LoadSurfaceTable(path)
	if err != nil {
		return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.config_invalid"), map[string]any{
			"path":  path,
			"error": domain.SafeError(err),
		})
	}
	missing, extra := config.DiffSurfaces(table)
	if len(missing) > 0 || len(extra) > 0 {
		return nil, domain.NewError(domain.ErrConfigInvalid, surfaceCheckMessage(missing, extra), map[string]any{
			"path":    path,
			"missing": missing,
			"extra":   extra,
		})
	}
	return map[string]any{
		"path":    path,
		"ok":      true,
		"missing": missing,
		"extra":   extra,
	}, nil
}

func surfaceCheckMessage(missing, extra []string) string {
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		parts = append(parts, "extra "+strings.Join(extra, ", "))
	}
	return "surface table does not match census: " + strings.Join(parts, "; ")
}

// resolvedPresets decorates each bundled preset with its catalog-resolved
// title and description. The Preset struct itself stays language-free
// (see task #82 §13) so JSON output is built here at the CLI boundary
// where the catalog is in scope.
func resolvedPresets(opts *runtimeOptions) []map[string]string {
	presets := config.ListPresets()
	out := make([]map[string]string, len(presets))
	for i, p := range presets {
		out[i] = map[string]string{
			"name":        p.Name,
			"repository":  p.Repository,
			"title":       opts.t("cli.preset." + p.Name + ".title"),
			"description": opts.t("cli.preset." + p.Name + ".description"),
		}
	}
	return out
}

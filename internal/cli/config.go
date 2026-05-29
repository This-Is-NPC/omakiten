package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// healthCheckRemediation pairs a validator error kind with the
// instructional command the user runs to repair the bundle. Every
// entry is read-only relative to user-owned files: the catalogue must
// never expand to include commands that mutate the user's config
// without their explicit invocation (#365 AC 9 / AC 10).
//
// `okt setup --update --skip-wrapper --skip-harnesses` overwrites
// shipped files only; user-owned `<kind>/custom/` subtrees stay
// untouched (verified at internal/config/default_files.go:84).
type healthCheckRemediation struct {
	Kind    string
	Command string
}

// healthCheckRemediations is the seeded catalogue per #365 AC 9. Keys
// the classifier may emit must each have an entry here so the envelope
// can never advertise a kind without a paired command.
var healthCheckRemediations = map[string]healthCheckRemediation{
	"missing_shipped_file":   {Kind: "missing_shipped_file", Command: "okt setup --update --skip-wrapper --skip-harnesses"},
	"embedded_default_drift": {Kind: "embedded_default_drift", Command: "okt setup --update --skip-wrapper --skip-harnesses"},
	"unknown_schema_key":     {Kind: "unknown_schema_key", Command: "okt config edit <path>"},
	"invalid_value":          {Kind: "invalid_value", Command: "okt config edit <path>"},
	"missing_required_key":   {Kind: "missing_required_key", Command: "okt config edit <path>"},
	"theme_not_found":        {Kind: "theme_not_found", Command: "okt config edit <path>"},
	"validation":             {Kind: "validation", Command: "okt config edit <path>"},
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
// classifier output without a matching healthCheckRemediations entry
// is a contract bug, not a user-facing fallback.
func classifyValidationError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "active theme") || strings.Contains(msg, "theme"):
		return "theme_not_found"
	case strings.Contains(msg, "unknown") && (strings.Contains(msg, "key") || strings.Contains(msg, "field")):
		return "unknown_schema_key"
	case strings.Contains(msg, "is required") || strings.Contains(msg, "must be set"):
		return "missing_required_key"
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
// #365 AC 1: `{errors: [{kind, path, message, suggested_command}],
// warnings: [...]}`. Callers compose this around the outer
// domain.NewError so `runJSON` writes an `ok: false` envelope with the
// structured payload nested under `details`.
func buildValidateFailureDetails(path string, err error, warnings []string) map[string]any {
	kind := classifyValidationError(err)
	entry := healthCheckRemediations[kind]
	return map[string]any{
		"path": path,
		"errors": []map[string]any{
			{
				"kind":              kind,
				"path":              path,
				"message":           domain.SafeError(err),
				"suggested_command": entry.Command,
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

	var validateMigrate bool
	validate := &cobra.Command{
		Use:   "validate [path]",
		Short: opts.t("cli.config.validate.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				path := ""
				if len(args) > 0 {
					path = args[0]
				} else {
					var err error
					path, err = opts.resolvedConfigPath()
					if err != nil {
						return nil, err
					}
				}

				if validateMigrate {
					rootDir, err := opts.resolvedConfigRoot()
					if err != nil {
						return nil, err
					}
					// MigrateLayout + EnsureDefaultFiles are both
					// idempotent and non-destructive on user-owned
					// files (`migrateSchemaDefaults` is additive,
					// `EnsureDefaultFiles` skips paths that already
					// exist). Running them as part of the health check
					// is the contract documented in #365 AC 1.
					if err := config.MigrateLayout(rootDir); err != nil {
						return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.config_invalid"), buildValidateFailureDetails(path, err, nil))
					}
					if err := config.EnsureDefaultFiles(rootDir); err != nil {
						return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.config_invalid"), buildValidateFailureDetails(path, err, nil))
					}
				}

				bundle, err := config.LoadBundle(path)
				if err != nil {
					if validateMigrate {
						return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.config_invalid"), buildValidateFailureDetails(path, err, nil))
					}
					return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.config_invalid"), map[string]any{"path": path, "error": domain.SafeError(err)})
				}
				if validateMigrate && bundle.ActiveThemeErr != nil {
					return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.theme_invalid"), buildValidateFailureDetails(path, bundle.ActiveThemeErr, extractBundleWarnings(bundle)))
				}
				if validateMigrate {
					return map[string]any{
						"path":     path,
						"errors":   []map[string]any{},
						"warnings": extractBundleWarnings(bundle),
					}, nil
				}
				return map[string]any{"path": path, "kit": bundle.Kit}, nil
			})
		},
	}
	validate.Flags().BoolVar(&validateMigrate, "migrate", false, opts.t("cli.config.validate.flag.migrate"))

	presets := &cobra.Command{
		Use:   "presets",
		Short: opts.t("cli.config.presets.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				return map[string]any{"presets": resolvedPresets(opts)}, nil
			})
		},
	}

	cmd.AddCommand(validate)
	cmd.AddCommand(presets)
	cmd.AddCommand(newConfigInitCommand(opts))
	cmd.AddCommand(newConfigShowCommand(opts))
	cmd.AddCommand(newConfigPathCommand(opts))
	cmd.AddCommand(newConfigWhyCommand(opts))
	cmd.AddCommand(newConfigDiffCommand(opts))
	cmd.AddCommand(newConfigLanguageCommand(opts))
	return cmd
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
			"title":       opts.t("cli.preset." + p.Name + ".title"),
			"description": opts.t("cli.preset." + p.Name + ".description"),
		}
	}
	return out
}

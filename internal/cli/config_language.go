package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// languageSourceLabel discriminates bundled vs custom in the show output.
const (
	languageSourceBundled = "bundled"
	languageSourceCustom  = "custom"
)

func newConfigLanguageCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "language",
		Short: "Manage the active language for CLI help, TUI labels, and the agent output directive",
		Long: `Read and write config.languages.{cli,tui,agent_output} in the active
omakiten.yaml. languages.cli and languages.tui are validated against
the language packs discovered under languages/; languages.agent_output
is free-form and forwarded verbatim to the MCP prompt composer.

The default target is the resolved omakiten.yaml (repo-local
.omakiten/ when present, otherwise the user-global ConfigRoot). Use
--global on set/reset to pin the write to the user-global file
regardless of repo-local presence.`,
	}
	cmd.AddCommand(newConfigLanguageShowCommand(opts))
	cmd.AddCommand(newConfigLanguageSetCommand(opts))
	cmd.AddCommand(newConfigLanguageResetCommand(opts))
	return cmd
}

func newConfigLanguageShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the active language settings and the discovered language packs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(_ context.Context) (any, error) {
				path, bundle, err := loadActiveBundle(opts)
				if err != nil {
					return nil, err
				}
				eff := bundle.Config.EffectiveLanguages()
				available := make([]map[string]string, 0, len(bundle.Languages))
				for _, lang := range bundle.Languages {
					src := languageSourceBundled
					if lang.IsCustom {
						src = languageSourceCustom
					}
					available = append(available, map[string]string{
						"code":   lang.Code,
						"name":   lang.Name,
						"native": lang.Native,
						"source": src,
					})
				}
				return map[string]any{
					"path": path,
					"languages": map[string]any{
						"cli":          eff.CLI,
						"tui":          eff.TUI,
						"agent_output": eff.AgentOutput,
					},
					"agent_output_note": "free-form; not validated against the loaded catalog",
					"available":         available,
				}, nil
			})
		},
	}
}

func newConfigLanguageSetCommand(opts *runtimeOptions) *cobra.Command {
	var (
		cli    string
		tui    string
		agent  string
		global bool
		// pointers track which flags the user actually passed so we can
		// distinguish "unset" from "explicitly empty" — empty agent is a
		// legitimate intent (clears the directive line).
		agentSet bool
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set one or more language settings in the active omakiten.yaml",
		Long: `Examples:
  okt config language set --cli pt-br
  okt config language set --tui en --agent "Português (Brasil)"
  okt config language set --agent "" --global

At least one of --cli, --tui, or --agent must be provided. --cli and
--tui validate against the language codes discovered under languages/.
--agent accepts any non-empty string (or "" to clear). --global pins
the write to the user-global omakiten.yaml.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliSet := cmd.Flags().Changed("cli")
			tuiSet := cmd.Flags().Changed("tui")
			agentSet = cmd.Flags().Changed("agent")
			return runJSON(cmd, func(_ context.Context) (any, error) {
				if !cliSet && !tuiSet && !agentSet {
					return nil, domain.NewError(domain.ErrValidation, "at least one of --cli, --tui, --agent must be supplied", nil)
				}
				path, err := writeTargetPath(opts, global)
				if err != nil {
					return nil, err
				}
				bundle, err := config.LoadBundle(path)
				if err != nil {
					return nil, domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
				}
				next := bundle.Config.Languages
				if cliSet {
					next.CLI = strings.TrimSpace(cli)
				}
				if tuiSet {
					next.TUI = strings.TrimSpace(tui)
				}
				if agentSet {
					next.AgentOutput = strings.TrimSpace(agent)
				}
				bundle.Config.Languages = next
				if err := validateLanguageWriteIntent(bundle, cliSet, tuiSet); err != nil {
					return nil, err
				}
				if err := config.SaveBundle(path, bundle); err != nil {
					return nil, fmt.Errorf("save %s: %w", path, err)
				}
				return map[string]any{
					"path":      path,
					"languages": settingsToMap(next),
				}, nil
			})
		},
	}
	cmd.Flags().StringVar(&cli, "cli", "", "language code for CLI help and usage strings")
	cmd.Flags().StringVar(&tui, "tui", "", "language code for TUI labels and screens")
	cmd.Flags().StringVar(&agent, "agent", "", "free-form agent output language directive (use \"\" to clear)")
	cmd.Flags().BoolVar(&global, "global", false, "pin the write to the user-global omakiten.yaml regardless of repo-local presence")
	return cmd
}

func newConfigLanguageResetCommand(opts *runtimeOptions) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove the languages block from the active omakiten.yaml (defaults apply)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(_ context.Context) (any, error) {
				path, err := writeTargetPath(opts, global)
				if err != nil {
					return nil, err
				}
				bundle, err := config.LoadBundle(path)
				if err != nil {
					return nil, domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
				}
				bundle.Config.Languages = config.LanguageSettings{}
				if err := config.SaveBundle(path, bundle); err != nil {
					return nil, fmt.Errorf("save %s: %w", path, err)
				}
				return map[string]any{
					"path":      path,
					"languages": settingsToMap(bundle.Config.EffectiveLanguages()),
				}, nil
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "pin the reset to the user-global omakiten.yaml regardless of repo-local presence")
	return cmd
}

// loadActiveBundle resolves the config path the resolver would pick and
// returns its loaded Bundle for the show command. Separate from
// writeTargetPath because show reads from the resolved path (which may
// be repo-local) while set/reset honor --global to bypass discovery.
func loadActiveBundle(opts *runtimeOptions) (string, config.Bundle, error) {
	path, err := opts.resolvedConfigPath()
	if err != nil {
		return "", config.Bundle{}, err
	}
	bundle, err := config.LoadBundle(path)
	if err != nil {
		return path, config.Bundle{}, domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
	}
	return path, bundle, nil
}

// writeTargetPath honors --global by routing the write to the
// user-global ConfigFile, except when an explicit --config flag was
// supplied — in that case the flag wins so test harnesses and scripted
// invocations can target a temp install regardless of --global.
// Without --global and without --config, the resolver-picked path
// wins, matching show.
func writeTargetPath(opts *runtimeOptions, global bool) (string, error) {
	if opts.configPath != "" {
		return opts.resolvedConfigPath()
	}
	if global {
		return paths.ConfigFile()
	}
	return opts.resolvedConfigPath()
}

// validateLanguageWriteIntent surfaces a clearer error than the
// bundle-load validator when a set call names a missing CLI/TUI code.
// The validator already runs inside SaveBundle (via the next LoadBundle
// on the path), but it points at the bundle file rather than the flag
// the user just typed. This early check trips before the file is
// touched.
func validateLanguageWriteIntent(bundle config.Bundle, cliSet, tuiSet bool) error {
	available := map[string]struct{}{}
	codes := make([]string, 0, len(bundle.Languages))
	for _, lang := range bundle.Languages {
		available[lang.Code] = struct{}{}
		codes = append(codes, lang.Code)
	}
	check := func(field, value string) error {
		v := strings.TrimSpace(value)
		if v == "" {
			return nil
		}
		if _, ok := available[v]; ok {
			return nil
		}
		return domain.NewError(domain.ErrValidation, fmt.Sprintf("--%s %q is not a loaded language code", field, v), map[string]any{"available": codes})
	}
	if cliSet {
		if err := check("cli", bundle.Config.Languages.CLI); err != nil {
			return err
		}
	}
	if tuiSet {
		if err := check("tui", bundle.Config.Languages.TUI); err != nil {
			return err
		}
	}
	// agent_output is free-form per AC §9 — no validation here.
	return nil
}

func settingsToMap(s config.LanguageSettings) map[string]any {
	return map[string]any{
		"cli":          s.CLI,
		"tui":          s.TUI,
		"agent_output": s.AgentOutput,
	}
}

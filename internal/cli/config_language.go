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
		Short: opts.t("cli.config.language.short"),
		Long: opts.t("cli.config.language.long"),
	}
	cmd.AddCommand(newConfigLanguageShowCommand(opts))
	cmd.AddCommand(newConfigLanguageSetCommand(opts))
	cmd.AddCommand(newConfigLanguageResetCommand(opts))
	return cmd
}

func newConfigLanguageShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: opts.t("cli.config.language.show.short"),
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
					"agent_output_note": opts.t("cli.print.agent_output_note"),
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
		Short: opts.t("cli.config.language.set.short"),
		Long: opts.t("cli.config.language.set.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliSet := cmd.Flags().Changed("cli")
			tuiSet := cmd.Flags().Changed("tui")
			agentSet = cmd.Flags().Changed("agent")
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				if !cliSet && !tuiSet && !agentSet {
					return nil, domain.NewError(domain.ErrValidation, opts.t("cli.err.language_set_no_flags"), nil)
				}
				path, err := writeTargetPath(opts, global)
				if err != nil {
					return nil, err
				}
				bundle, err := config.LoadBundle(path)
				if err != nil {
					return nil, domain.NewError(domain.ErrConfigInvalid, opts.t("cli.err.config_invalid"), map[string]any{"path": path, "error": fmt.Sprint(err)})
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
				if err := reloadActiveBundle(ctx, opts); err != nil {
					return nil, err
				}
				return map[string]any{
					"path":      path,
					"languages": settingsToMap(next),
				}, nil
			})
		},
	}
	cmd.Flags().StringVar(&cli, "cli", "", opts.t("cli.config.language.set.flag.cli"))
	cmd.Flags().StringVar(&tui, "tui", "", opts.t("cli.config.language.set.flag.tui"))
	cmd.Flags().StringVar(&agent, "agent", "", opts.t("cli.config.language.set.flag.agent"))
	cmd.Flags().BoolVar(&global, "global", false, opts.t("cli.config.language.set.flag.global"))
	return cmd
}

func newConfigLanguageResetCommand(opts *runtimeOptions) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: opts.t("cli.config.language.reset.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				path, err := writeTargetPath(opts, global)
				if err != nil {
					return nil, err
				}
				bundle, err := config.LoadBundle(path)
				if err != nil {
					return nil, domain.NewError(domain.ErrConfigInvalid, opts.t("cli.err.config_invalid"), map[string]any{"path": path, "error": fmt.Sprint(err)})
				}
				bundle.Config.Languages = config.LanguageSettings{}
				if err := config.SaveBundle(path, bundle); err != nil {
					return nil, fmt.Errorf("save %s: %w", path, err)
				}
				if err := reloadActiveBundle(ctx, opts); err != nil {
					return nil, err
				}
				return map[string]any{
					"path":      path,
					"languages": settingsToMap(bundle.Config.EffectiveLanguages()),
				}, nil
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, opts.t("cli.config.language.reset.flag.global"))
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
		return path, config.Bundle{}, domain.NewError(domain.ErrConfigInvalid, opts.t("cli.err.config_invalid"), map[string]any{"path": path, "error": fmt.Sprint(err)})
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
		return domain.NewError(domain.ErrValidation, fmt.Sprintf(t("cli.err.unknown_language_code"), field, v), map[string]any{"available": codes})
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

// reloadActiveBundle honors AC §25: after a successful write the
// command rebuilds the runtime's BundleCache entry so anything sharing
// the cache (TUI started in-process, future MCP server embedded in the
// same process tree) observes the new selection without restart. For a
// short-lived CLI invocation the rebuild also doubles as a post-write
// validation pass — a broken bundle that somehow round-trips SaveBundle
// would surface here before the command returns success.
//
// The cache rebuilds against rt.configPath (the runtime's own boot
// source), not the write target: when --global is supplied inside a
// repo with .omakiten/, the active cache still holds repo-local and
// re-parsing global would replace the wrong entry. Other long-running
// consumers pick up changes through BundleCache.Resolve's mtime check
// against their own source path, which the atomic write naturally bumps.
func reloadActiveBundle(ctx context.Context, opts *runtimeOptions) error {
	rt, err := opts.open(ctx, true)
	if err != nil {
		return fmt.Errorf("reload bundle: %w", err)
	}
	defer rt.close()
	if rt.cache == nil {
		return nil
	}
	if _, err := rt.cache.Reload(ctx, rt.projectID, rt.configPath); err != nil {
		return fmt.Errorf("reload bundle: %w", err)
	}
	return nil
}

func settingsToMap(s config.LanguageSettings) map[string]any {
	return map[string]any{
		"cli":          s.CLI,
		"tui":          s.TUI,
		"agent_output": s.AgentOutput,
	}
}

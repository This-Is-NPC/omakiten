package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
	"omakiten/internal/paths"
)

// setupInputs collects the five user-controllable values the picker
// would normally resolve from screens. The headless path fills the
// struct from env vars + flags; the upcoming bubbletea path will fill
// it from picker results. Both paths feed runSetup so the side-effects
// (yaml write, .active marker, rc wrapper, harness configuration)
// stay in one place.
type setupInputs struct {
	CLILang     string
	TUILang     string
	AgentLang   string
	AgentLangSet bool
	Preset      string
	Harnesses   []string
	HarnessesSet bool
}

func newSetupCommand(opts *runtimeOptions) *cobra.Command {
	var (
		update      bool
		cliLang     string
		tuiLang     string
		agentLang   string
		presetName  string
		harnessesCSV string
		skipWrapper bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: opts.t("cli.setup.short"),
		Long:  opts.t("cli.setup.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			agentSet := cmd.Flags().Changed("agent-lang") || envSet("OKT_AGENT_LANG")
			harnessSet := cmd.Flags().Changed("harnesses") || envSet("OKT_HARNESSES")

			inputs, err := resolveSetupInputs(cmd, setupFlagValues{
				CLILang:      cliLang,
				TUILang:      tuiLang,
				AgentLang:    agentLang,
				AgentLangSet: agentSet,
				Preset:       presetName,
				HarnessesCSV: harnessesCSV,
				HarnessesSet: harnessSet,
			})
			if err != nil {
				return writeError(cmd, err)
			}

			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runSetup(ctx, opts, inputs, runSetupOptions{Update: update, SkipWrapper: skipWrapper})
			})
		},
	}

	cmd.Flags().BoolVar(&update, "update", false, opts.t("cli.setup.flag.update"))
	cmd.Flags().StringVar(&cliLang, "cli-lang", "", opts.t("cli.setup.flag.cli-lang"))
	cmd.Flags().StringVar(&tuiLang, "tui-lang", "", opts.t("cli.setup.flag.tui-lang"))
	cmd.Flags().StringVar(&agentLang, "agent-lang", "", opts.t("cli.setup.flag.agent-lang"))
	cmd.Flags().StringVar(&presetName, "preset", "", opts.t("cli.setup.flag.preset"))
	cmd.Flags().StringVar(&harnessesCSV, "harnesses", "", opts.t("cli.setup.flag.harnesses"))
	cmd.Flags().BoolVar(&skipWrapper, "skip-wrapper", false, opts.t("cli.setup.flag.skip-wrapper"))

	return cmd
}

type setupFlagValues struct {
	CLILang      string
	TUILang      string
	AgentLang    string
	AgentLangSet bool
	Preset       string
	HarnessesCSV string
	HarnessesSet bool
}

// resolveSetupInputs collapses flags + env vars into setupInputs.
// Flag values win over env vars (cobra has already filled the flag
// fields with the user-supplied or default values; cmd.Flags().Changed
// is the only way to tell explicit `--cli-lang en` from "user didn't
// pass this flag, use the env var if set"). The interactive picker is
// deferred to a follow-up commit — for now, any missing input
// triggers a coded error pointing the user at the env vars / flags.
func resolveSetupInputs(cmd *cobra.Command, flags setupFlagValues) (setupInputs, error) {
	inputs := setupInputs{}

	inputs.CLILang = firstNonEmpty(flagOrEnv(cmd, "cli-lang", flags.CLILang, "OKT_CLI_LANG"), "")
	inputs.TUILang = firstNonEmpty(flagOrEnv(cmd, "tui-lang", flags.TUILang, "OKT_TUI_LANG"), inputs.CLILang)

	if flags.AgentLangSet {
		if cmd.Flags().Changed("agent-lang") {
			inputs.AgentLang = strings.TrimSpace(flags.AgentLang)
		} else {
			inputs.AgentLang = strings.TrimSpace(os.Getenv("OKT_AGENT_LANG"))
		}
		inputs.AgentLangSet = true
	}

	rawPreset := flagOrEnv(cmd, "preset", flags.Preset, "OKT_PRESET")
	resolvedPreset, fellback := installer.ResolvePreset(rawPreset)
	if fellback && rawPreset != "" {
		// Warn but do not fail — install.sh's select_preset prints the
		// same line on unknown OKT_PRESET= and continues with the
		// default; preserving that contract avoids breaking pinned
		// curl|bash invocations that misspelled the preset name.
		fmt.Fprintf(cmd.ErrOrStderr(), t("cli.setup.warn.unknown_preset")+"\n", rawPreset, installer.DefaultPreset)
	}
	inputs.Preset = resolvedPreset

	if flags.HarnessesSet {
		raw := flagOrEnv(cmd, "harnesses", flags.HarnessesCSV, "OKT_HARNESSES")
		harnesses, status, warnings := installer.ParseHarnessSelection(raw)
		for _, w := range warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), w)
		}
		switch status {
		case installer.StatusOK, installer.StatusSkip:
			inputs.Harnesses = harnesses
		case installer.StatusInvalid, installer.StatusEmpty:
			// Empty / all-invalid CSV is treated as "configure nothing"
			// for the headless path — matches install.sh's silent
			// no-TTY behaviour where empty/garbage input yields no
			// harness setup rather than aborting the install.
			inputs.Harnesses = nil
		}
		inputs.HarnessesSet = true
	}

	if inputs.CLILang == "" || inputs.TUILang == "" || !inputs.AgentLangSet || !inputs.HarnessesSet {
		return setupInputs{}, domain.NewError(domain.ErrValidation, t("cli.setup.short")+": interactive picker not yet wired; supply env vars (OKT_CLI_LANG/OKT_TUI_LANG/OKT_AGENT_LANG/OKT_PRESET/OKT_HARNESSES) or flags (--cli-lang/--tui-lang/--agent-lang/--preset/--harnesses) on this invocation", map[string]any{
			"cli_lang_set":   inputs.CLILang != "",
			"tui_lang_set":   inputs.TUILang != "",
			"agent_lang_set": inputs.AgentLangSet,
			"harnesses_set":  inputs.HarnessesSet,
		})
	}
	return inputs, nil
}

type runSetupOptions struct {
	Update      bool
	SkipWrapper bool
}

// runSetup applies the resolved inputs to the user-global install
// (seed the config root if missing, point .active at the chosen
// preset, write the languages block into omakiten.yaml, install the
// shell-rc wrapper, configure each selected MCP harness). Idempotent
// against an existing install: re-running with --update overwrites
// language fields and the wrapper block in place without touching
// other yaml fields or unrelated rc-file content.
func runSetup(ctx context.Context, opts *runtimeOptions, inputs setupInputs, runOpts runSetupOptions) (any, error) {
	rootDir, err := paths.ConfigRoot()
	if err != nil {
		return nil, err
	}
	seedRes, err := config.SeedInstall(rootDir, inputs.Preset, false)
	if err != nil {
		return nil, presetCLIError(opts, err)
	}

	bundle, err := config.LoadBundle(seedRes.Path)
	if err != nil {
		return nil, domain.NewError(domain.ErrConfigInvalid, t("cli.err.init_seeded_config_invalid"), map[string]any{"path": seedRes.Path, "error": fmt.Sprint(err)})
	}
	bundle.Config.Languages = config.LanguageSettings{
		CLI:         inputs.CLILang,
		TUI:         inputs.TUILang,
		AgentOutput: inputs.AgentLang,
	}
	if err := validateSetupLanguageChoice("cli-lang", inputs.CLILang, bundle.Languages); err != nil {
		return nil, err
	}
	if err := validateSetupLanguageChoice("tui-lang", inputs.TUILang, bundle.Languages); err != nil {
		return nil, err
	}
	if err := config.SaveBundle(seedRes.Path, bundle); err != nil {
		return nil, fmt.Errorf("save %s: %w", seedRes.Path, err)
	}

	activeDir, err := installer.WriteActivePreset(inputs.Preset)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"root":   rootDir,
		"preset": map[string]any{"name": inputs.Preset, "path": seedRes.Path, "active_dir": activeDir},
		"languages": map[string]any{
			"cli":          inputs.CLILang,
			"tui":          inputs.TUILang,
			"agent_output": inputs.AgentLang,
		},
		"update": runOpts.Update,
	}

	if !runOpts.SkipWrapper {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		installedInto, err := installer.WriteWrappers(home)
		if err != nil {
			return nil, err
		}
		result["wrapper"] = map[string]any{"installed_into": installedInto}
	}

	if len(inputs.Harnesses) > 0 {
		oktBin, err := os.Executable()
		if err != nil {
			oktBin = "okt"
		}
		harnessResults := installer.SetupHarnesses(ctx, oktBin, inputs.Harnesses)
		summary := make([]map[string]any, 0, len(harnessResults))
		for _, r := range harnessResults {
			entry := map[string]any{
				"harness":   r.Harness,
				"status":    r.Status,
				"exit_code": r.ExitCode,
			}
			if r.Err != nil {
				entry["error"] = r.Err.Error()
			}
			summary = append(summary, entry)
		}
		result["harnesses"] = summary
	}

	return result, nil
}

// validateSetupLanguageChoice is the setup-surface equivalent of
// validateInitLanguageChoice — it rejects unknown codes against the
// freshly-loaded bundle.Languages so a typo like `OKT_CLI_LANG=ne`
// surfaces as a coded error rather than silently activating the
// catalog fallback chain at runtime.
func validateSetupLanguageChoice(flag, value string, available []config.Language) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	for _, lang := range available {
		if lang.Code == v {
			return nil
		}
	}
	codes := make([]string, 0, len(available))
	for _, lang := range available {
		codes = append(codes, lang.Code)
	}
	return domain.NewError(domain.ErrValidation, fmt.Sprintf(t("cli.err.unknown_language_code"), flag, v), map[string]any{"available": codes})
}

// flagOrEnv returns the flag value when the user explicitly supplied
// it, falling back to the env var otherwise. Empty results bubble up
// so the caller can decide whether "" means "use the default" or
// "the picker still needs to ask".
func flagOrEnv(cmd *cobra.Command, flagName, flagValue, envName string) string {
	if cmd.Flags().Changed(flagName) {
		return strings.TrimSpace(flagValue)
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func envSet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}


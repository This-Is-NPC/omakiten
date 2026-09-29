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
// resolves from screens. The headless path fills the struct from env
// vars + flags; the interactive path fills it from picker results.
// Both paths feed runSetup so the side-effects (yaml write, .active
// marker, rc wrapper, harness configuration) stay in one place.
//
// The *Set booleans distinguish "the user supplied an explicit empty
// value" from "the user did not supply this input at all". Without
// them the picker cannot tell whether an empty agent-lang means
// "leave blank" (skip prompt) or "ask me".
type setupInputs struct {
	CLILang      string
	TUILang      string
	AgentLang    string
	AgentLangSet bool
	Preset       string
	Harnesses    []string
	HarnessesSet bool
}

func newSetupCommand(opts *runtimeOptions) *cobra.Command {
	var (
		update        bool
		cliLang       string
		tuiLang       string
		agentLang     string
		presetName    string
		harnessesCSV  string
		skipWrapper   bool
		skipHarnesses bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: opts.t("cli.setup.short"),
		Long:  opts.t("cli.setup.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			agentSet := cmd.Flags().Changed("agent-lang") || envSet("OKT_AGENT_LANG")
			harnessSet := cmd.Flags().Changed("harnesses") || envSet("OKT_HARNESSES")
			presetSet := cmd.Flags().Changed("preset") || envSet("OKT_PRESET")

			inputs, needs := resolveSetupInputs(cmd, setupFlagValues{
				CLILang:      cliLang,
				TUILang:      tuiLang,
				AgentLang:    agentLang,
				AgentLangSet: agentSet,
				Preset:       presetName,
				PresetSet:    presetSet,
				HarnessesCSV: harnessesCSV,
				HarnessesSet: harnessSet,
			})
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				finalInputs, err := runSetupPicker(ctx, inputs, needs)
				if err != nil {
					return nil, err
				}
				return runSetup(ctx, opts, finalInputs, runSetupOptions{Update: update, SkipWrapper: skipWrapper, SkipHarnesses: skipHarnesses})
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
	cmd.Flags().BoolVar(&skipHarnesses, "skip-harnesses", false, opts.t("cli.setup.flag.skip-harnesses"))
	cmd.MarkFlagsMutuallyExclusive("skip-harnesses", "harnesses")

	return cmd
}

type setupFlagValues struct {
	CLILang      string
	TUILang      string
	AgentLang    string
	AgentLangSet bool
	Preset       string
	PresetSet    bool
	HarnessesCSV string
	HarnessesSet bool
}

// resolveSetupInputs collapses flags + env vars into a partial
// setupInputs plus a pickerNeeds mask. Each `OKT_*` env var (or the
// matching flag) flips its bit to false in the mask so the interactive
// picker collapses screens whose value was already supplied. The
// surrounding cobra RunE feeds the partial into runSetupPicker, which
// either returns the partial unchanged (every input present) or
// drives the bubbletea program to fill in the gaps.
//
// The TUI-lang default ("if user supplied CLI but not TUI, the TUI
// defaults to the CLI choice") still lives here so the headless path
// preserves the existing contract — picker callers see TUILang
// pre-populated from CLI and adjust if they want.
func resolveSetupInputs(cmd *cobra.Command, flags setupFlagValues) (setupInputs, pickerNeeds) {
	inputs := setupInputs{}
	needs := pickerNeeds{}
	resolveSetupLanguages(cmd, flags, &inputs, &needs)
	resolveSetupAgentLanguage(cmd, flags, &inputs, &needs)
	resolveSetupPreset(cmd, flags, &inputs, &needs)
	resolveSetupHarnesses(cmd, flags, &inputs, &needs)
	return inputs, needs
}

func resolveSetupLanguages(cmd *cobra.Command, flags setupFlagValues, inputs *setupInputs, needs *pickerNeeds) {
	// CLI + TUI share a single picker screen on install; the per-surface
	// split lives in omakiten.yaml so the user can override later via
	// `okt config language`. If either env var is set we treat both as
	// resolved (CLILang takes precedence; TUILang mirrors it when only
	// CLI is set, and vice versa).
	cliRaw := flagOrEnv(cmd, "cli-lang", flags.CLILang, "OKT_CLI_LANG")
	tuiRaw := flagOrEnv(cmd, "tui-lang", flags.TUILang, "OKT_TUI_LANG")
	switch {
	case cliRaw != "":
		inputs.CLILang = cliRaw
		if tuiRaw != "" {
			inputs.TUILang = tuiRaw
		} else {
			inputs.TUILang = cliRaw
		}
	case tuiRaw != "":
		inputs.CLILang = tuiRaw
		inputs.TUILang = tuiRaw
	default:
		needs.Lang = true
	}
}

func resolveSetupAgentLanguage(cmd *cobra.Command, flags setupFlagValues, inputs *setupInputs, needs *pickerNeeds) {
	if flags.AgentLangSet {
		if cmd.Flags().Changed("agent-lang") {
			inputs.AgentLang = strings.TrimSpace(flags.AgentLang)
		} else {
			inputs.AgentLang = strings.TrimSpace(os.Getenv("OKT_AGENT_LANG"))
		}
		inputs.AgentLangSet = true
	} else {
		needs.Agent = true
	}
}

func resolveSetupPreset(cmd *cobra.Command, flags setupFlagValues, inputs *setupInputs, needs *pickerNeeds) {
	rawPreset := flagOrEnv(cmd, "preset", flags.Preset, "OKT_PRESET")
	if flags.PresetSet || rawPreset != "" {
		resolvedPreset, fellback := installer.ResolvePreset(rawPreset)
		if fellback && rawPreset != "" {
			// Warn but do not fail — install.sh's select_preset prints
			// the same line on unknown OKT_PRESET= and continues with
			// the default; preserving that contract avoids breaking
			// pinned curl|bash invocations that misspelled the name.
			fmt.Fprintf(cmd.ErrOrStderr(), t("cli.setup.warn.unknown_preset")+"\n", rawPreset, installer.DefaultPreset)
		}
		inputs.Preset = resolvedPreset
	} else {
		needs.Preset = true
	}
}

func resolveSetupHarnesses(cmd *cobra.Command, flags setupFlagValues, inputs *setupInputs, needs *pickerNeeds) {
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
	} else {
		needs.Harness = true
	}
}

type runSetupOptions struct {
	Update        bool
	SkipWrapper   bool
	SkipHarnesses bool
}

// runSetup installs the selected configuration, languages, wrapper and skills.
// Update refreshes managed files and preserves unrelated user content.
func runSetup(ctx context.Context, opts *runtimeOptions, inputs setupInputs, runOpts runSetupOptions) (any, error) {
	rootDir, seedRes, activeDir, err := prepareSetupConfig(ctx, opts, inputs, runOpts.Update)
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
		wrapper, err := setupWrappers()
		if err != nil {
			return nil, err
		}
		result["wrapper"] = wrapper
	}

	result["harnesses_planned"] = inputs.Harnesses
	if len(inputs.Harnesses) > 0 && !runOpts.SkipHarnesses {
		skills, err := installer.InstallSkills("", inputs.Harnesses, runOpts.Update)
		if err != nil {
			return nil, err
		}
		result["skills"] = skills
	}

	return result, nil
}

func prepareSetupConfig(ctx context.Context, opts *runtimeOptions, inputs setupInputs, update bool) (string, config.PresetResult, string, error) {
	rootDir, err := paths.ConfigRoot()
	if err != nil {
		return "", config.PresetResult{}, "", err
	}
	seedRes, err := installer.InstallPreset(ctx, rootDir, inputs.Preset, update)
	if err != nil {
		return "", config.PresetResult{}, "", presetCLIError(opts, err)
	}
	bundle, err := config.LoadBundle(seedRes.Path)
	if err != nil {
		return "", config.PresetResult{}, "", domain.NewError(domain.ErrConfigInvalid, t("cli.err.init_seeded_config_invalid"), map[string]any{"path": seedRes.Path, "error": fmt.Sprint(err)})
	}
	languages := config.LanguageSettings{CLI: inputs.CLILang, TUI: inputs.TUILang, AgentOutput: inputs.AgentLang}
	for _, choice := range []struct{ flag, value string }{{"cli-lang", inputs.CLILang}, {"tui-lang", inputs.TUILang}} {
		if err := validateInitLanguageChoice(choice.flag, choice.value, availableLanguageCodes(bundle.Languages)); err != nil {
			return "", config.PresetResult{}, "", err
		}
	}
	if err := config.SavePreferences(config.Preferences{Languages: languages}); err != nil {
		return "", config.PresetResult{}, "", fmt.Errorf("save application preferences: %w", err)
	}
	return rootDir, seedRes, rootDir, nil
}

func setupWrappers() (map[string]any, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	installedInto, err := installer.WriteWrappers(home)
	if err != nil {
		return nil, err
	}
	psInstalledInto, err := installer.WritePowerShellWrappers(home)
	if err != nil {
		return nil, err
	}
	return map[string]any{"installed_into": installedInto, "powershell_installed_into": psInstalledInto}, nil
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

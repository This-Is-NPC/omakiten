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

			inputs, needs, err := resolveSetupInputs(cmd, setupFlagValues{
				CLILang:      cliLang,
				TUILang:      tuiLang,
				AgentLang:    agentLang,
				AgentLangSet: agentSet,
				Preset:       presetName,
				PresetSet:    presetSet,
				HarnessesCSV: harnessesCSV,
				HarnessesSet: harnessSet,
			})
			if err != nil {
				return writeError(cmd, err)
			}

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
func resolveSetupInputs(cmd *cobra.Command, flags setupFlagValues) (setupInputs, pickerNeeds, error) {
	inputs := setupInputs{}
	needs := pickerNeeds{}

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

	return inputs, needs, nil
}

type runSetupOptions struct {
	Update        bool
	SkipWrapper   bool
	SkipHarnesses bool
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

	result["harnesses_planned"] = inputs.Harnesses
	if len(inputs.Harnesses) > 0 && !runOpts.SkipHarnesses {
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


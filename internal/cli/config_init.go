package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
	"omakiten/internal/paths"
)

func newConfigInitCommand(opts *runtimeOptions) *cobra.Command {
	var scopeFlag string
	var presetName string
	var force bool
	var cliLang string
	var tuiLang string
	var agentLang string

	cmd := &cobra.Command{
		Use:   "init",
		Short: opts.t("cli.config.init.short"),
		Long:  opts.t("cli.config.init.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigInit(cmd, opts, configInitInputs{
				scope:      scopeFlag,
				preset:     presetName,
				force:      force,
				cliLang:    cliLang,
				tuiLang:    tuiLang,
				agentLang:  agentLang,
				cliLangSet: cmd.Flags().Changed("cli-lang"),
				tuiLangSet: cmd.Flags().Changed("tui-lang"),
				agentSet:   cmd.Flags().Changed("agent-lang"),
			})
		},
	}
	cmd.Flags().StringVar(&scopeFlag, "scope", "", opts.t("cli.config.init.flag.scope"))
	cmd.Flags().StringVar(&presetName, "preset", "", opts.t("cli.config.init.flag.preset"))
	cmd.Flags().BoolVar(&force, "force", false, opts.t("cli.config.init.flag.force"))
	cmd.Flags().StringVar(&cliLang, "cli-lang", "", opts.t("cli.config.init.flag.cli-lang"))
	cmd.Flags().StringVar(&tuiLang, "tui-lang", "", opts.t("cli.config.init.flag.tui-lang"))
	cmd.Flags().StringVar(&agentLang, "agent-lang", "", opts.t("cli.config.init.flag.agent-lang"))
	_ = cmd.MarkFlagRequired("scope")
	_ = cmd.MarkFlagRequired("preset")
	return cmd
}

type configInitInputs struct {
	scope      string
	preset     string
	force      bool
	cliLang    string
	tuiLang    string
	agentLang  string
	cliLangSet bool
	tuiLangSet bool
	agentSet   bool
}

func runConfigInit(cmd *cobra.Command, opts *runtimeOptions, inputs configInitInputs) error {
	return runJSON(cmd, func(ctx context.Context) (any, error) {
		root, err := resolveScopeRoot(opts, inputs.scope)
		if err != nil {
			return nil, err
		}
		res, err := installer.InstallPreset(ctx, root, inputs.preset, inputs.force)
		if err != nil {
			return nil, presetCLIError(opts, err)
		}
		langSummary, err := applyLanguageSelections(cmd, languagePromptInputs{
			CLILangSet:   inputs.cliLangSet,
			CLILang:      inputs.cliLang,
			TUILangSet:   inputs.tuiLangSet,
			TUILang:      inputs.tuiLang,
			AgentLangSet: inputs.agentSet,
			AgentLang:    inputs.agentLang,
		})
		if err != nil {
			return nil, err
		}
		return configInitPayload(inputs.scope, root, res, langSummary), nil
	})
}

func configInitPayload(scope, root string, res config.PresetResult, langSummary map[string]any) map[string]any {
	payload := map[string]any{
		"scope": scope,
		"root":  root,
		"preset": map[string]any{
			"name": res.PresetName,
			"path": res.Path,
		},
	}
	if res.NoOp {
		payload["no_op"] = true
	}
	if res.Refreshed {
		payload["refreshed"] = true
	}
	if langSummary != nil {
		payload["languages"] = langSummary
	}
	return payload
}

// languagePromptInputs carries the flag values the init RunE collected
// from cobra into applyLanguageSelections. Bool fields capture whether
// the user supplied the flag at all so empty values can be told apart
// from omitted ones — `--agent-lang ""` is a legitimate explicit
// clear, while omitting the flag leaves the surface at its current
// configured value (or invokes the TTY prompt).
type languagePromptInputs struct {
	CLILangSet   bool
	CLILang      string
	TUILangSet   bool
	TUILang      string
	AgentLangSet bool
	AgentLang    string
}

// applyLanguageSelections persists application-wide language choices.
func applyLanguageSelections(cmd *cobra.Command, inputs languagePromptInputs) (map[string]any, error) {
	preferences, err := loadApplicationPreferences()
	if err != nil {
		return nil, err
	}
	languages, err := config.LoadBundledLanguages()
	if err != nil {
		return nil, err
	}
	available := availableLanguageCodes(languages)
	defaults := preferences.Languages
	next, err := resolveInitLanguages(cmd, inputs, available, defaults)
	if err != nil {
		return nil, err
	}
	if next == defaults {
		return nil, nil
	}
	for _, choice := range []struct{ flag, value string }{{"cli-lang", next.CLI}, {"tui-lang", next.TUI}} {
		if err := validateInitLanguageChoice(choice.flag, choice.value, available); err != nil {
			return nil, err
		}
	}
	if err := config.SavePreferences(config.Preferences{Languages: next}); err != nil {
		return nil, err
	}
	return settingsToMap(next), nil
}

func resolveInitLanguages(cmd *cobra.Command, inputs languagePromptInputs, available []string, defaults config.LanguageSettings) (config.LanguageSettings, error) {
	cmd.SetIn(bufio.NewReader(cmd.InOrStdin()))
	next := defaults
	var err error
	next.CLI, err = resolveLanguageCode(cmd, inputs.CLILangSet, inputs.CLILang, defaults.CLI, available, "cli")
	if err != nil {
		return config.LanguageSettings{}, err
	}
	next.TUI, err = resolveLanguageCode(cmd, inputs.TUILangSet, inputs.TUILang, defaults.TUI, available, "tui")
	if err != nil {
		return config.LanguageSettings{}, err
	}
	next.AgentOutput, err = resolveAgentLanguage(cmd, inputs.AgentLangSet, inputs.AgentLang, defaults.AgentOutput)
	if err != nil {
		return config.LanguageSettings{}, err
	}
	return next, nil
}

func resolveLanguageCode(cmd *cobra.Command, set bool, value, fallback string, available []string, surface string) (string, error) {
	if set {
		return strings.TrimSpace(value), nil
	}
	if !isInteractive(cmd) {
		return fallback, nil
	}
	return promptLanguageCode(cmd, t("cli.print.prompt_label."+surface), available, fallback)
}

func resolveAgentLanguage(cmd *cobra.Command, set bool, value, fallback string) (string, error) {
	if set {
		return strings.TrimSpace(value), nil
	}
	if !isInteractive(cmd) {
		return fallback, nil
	}
	return promptFreeForm(cmd, t("cli.print.prompt_label.agent"), fallback)
}

func availableLanguageCodes(langs []config.Language) []string {
	out := make([]string, 0, len(langs))
	for _, lang := range langs {
		out = append(out, lang.Code)
	}
	sort.Strings(out)
	return out
}

func validateInitLanguageChoice(flag, value string, available []string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	for _, code := range available {
		if code == v {
			return nil
		}
	}
	return domain.NewError(domain.ErrValidation, fmt.Sprintf(t("cli.err.unknown_language_code"), flag, v), map[string]any{"available": available})
}

// isInteractive enables prompts when input and output belong to a terminal session.
func isInteractive(cmd *cobra.Command) bool {
	return stdinIsTTY() && streamIsTTY(cmd.ErrOrStderr())
}

// promptLanguageCode prints the available codes and reads one from
// stdin, defaulting to fallback when the user submits an empty line.
// Loops until the entry matches an available code so the seeded
// omakiten.yaml never lands with an invalid value.
func promptLanguageCode(cmd *cobra.Command, label string, available []string, fallback string) (string, error) {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.ErrOrStderr()
	def := fallback
	if def == "" {
		def = "en"
	}
	for {
		fmt.Fprintf(out, t("cli.print.prompt_with_options"), label, strings.Join(available, ", "), def)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		choice := strings.TrimSpace(line)
		if choice == "" {
			return def, nil
		}
		for _, code := range available {
			if code == choice {
				return choice, nil
			}
		}
		fmt.Fprintf(out, t("cli.print.prompt_unknown_code"), choice, strings.Join(available, ", "))
		if err == io.EOF {
			return "", domain.NewError(domain.ErrValidation, fmt.Sprintf(t("cli.err.unknown_language_code"), label, choice), map[string]any{"available": available})
		}
	}
}

// promptFreeForm reads any text from stdin, defaulting to fallback
// when the user submits an empty line. Used for languages.agent_output
// which is a directive consumed by the agent, not a catalog key.
func promptFreeForm(cmd *cobra.Command, label, fallback string) (string, error) {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.ErrOrStderr()
	def := fallback
	defLabel := def
	if defLabel == "" {
		defLabel = t("cli.print.prompt_freeform_none")
	}
	fmt.Fprintf(out, t("cli.print.prompt_freeform"), label, defLabel)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return def, nil
	}
	return choice, nil
}

// resolveScopeRoot returns the directory preset installation should populate for the
// chosen scope. Global honours --config (deriving the ConfigRoot via
// ConfigRootFromYAMLPath) and otherwise falls back to paths.ConfigRoot();
// local writes to <cwd>/.omakiten literally without walk-up so monorepos
// place the install exactly where the user invoked the command.
func resolveScopeRoot(opts *runtimeOptions, scope string) (string, error) {
	switch scope {
	case "global":
		if opts.configPath != "" {
			abs, err := filepath.Abs(opts.configPath)
			if err != nil {
				return "", err
			}
			if filepath.Base(abs) == config.PresetSelectionFile {
				return filepath.Dir(abs), nil
			}
			return config.ConfigRootFromYAMLPath(abs), nil
		}
		return paths.ConfigRoot()
	case "local":
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		if opts.discoveryStart != "" {
			cwd = opts.discoveryStart
		}
		root := filepath.Join(cwd, config.RepoLocalDirName)
		if err := config.ValidateRepoLocalRoot(root); err != nil {
			return "", err
		}
		return root, nil
	default:
		return "", domain.NewError(domain.ErrValidation, t("cli.err.invalid_scope"), map[string]any{"scope": scope})
	}
}

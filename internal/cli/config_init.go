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
		Long: opts.t("cli.config.init.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliLangSet := cmd.Flags().Changed("cli-lang")
			tuiLangSet := cmd.Flags().Changed("tui-lang")
			agentLangSet := cmd.Flags().Changed("agent-lang")
			return runJSON(cmd, func(context.Context) (any, error) {
				root, err := resolveScopeRoot(opts, scopeFlag)
				if err != nil {
					return nil, err
				}
				res, err := config.SeedInstall(root, presetName, force)
				if err != nil {
					return nil, presetCLIError(err)
				}
				langSummary, err := applyLanguageSelections(cmd, res.Path, languagePromptInputs{
					CLILangSet:   cliLangSet,
					CLILang:      cliLang,
					TUILangSet:   tuiLangSet,
					TUILang:      tuiLang,
					AgentLangSet: agentLangSet,
					AgentLang:    agentLang,
				})
				if err != nil {
					return nil, err
				}
				payload := map[string]any{
					"scope": scopeFlag,
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
				return payload, nil
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

// applyLanguageSelections loads the freshly-seeded omakiten.yaml,
// resolves the desired per-surface language values from the supplied
// flags (or, when missing on an interactive TTY, prompts the user),
// validates CLI/TUI codes against the discovered language packs, and
// rewrites the file with the new languages block. Returns a summary
// map for the JSON payload so callers can confirm which values
// actually landed. Returns nil when no change is needed.
func applyLanguageSelections(cmd *cobra.Command, configPath string, inputs languagePromptInputs) (map[string]any, error) {
	bundle, err := config.LoadBundle(configPath)
	if err != nil {
		return nil, domain.NewError(domain.ErrConfigInvalid, "init seeded config is invalid", map[string]any{"path": configPath, "error": fmt.Sprint(err)})
	}
	available := availableLanguageCodes(bundle.Languages)
	defaults := bundle.Config.Languages
	next := defaults

	if inputs.CLILangSet {
		next.CLI = strings.TrimSpace(inputs.CLILang)
	} else if isInteractive(cmd) {
		choice, err := promptLanguageCode(cmd, "CLI language", available, defaults.CLI)
		if err != nil {
			return nil, err
		}
		next.CLI = choice
	}
	if inputs.TUILangSet {
		next.TUI = strings.TrimSpace(inputs.TUILang)
	} else if isInteractive(cmd) {
		choice, err := promptLanguageCode(cmd, "TUI language", available, defaults.TUI)
		if err != nil {
			return nil, err
		}
		next.TUI = choice
	}
	if inputs.AgentLangSet {
		next.AgentOutput = strings.TrimSpace(inputs.AgentLang)
	} else if isInteractive(cmd) {
		choice, err := promptFreeForm(cmd, "Agent output language (free-form, blank to skip)", defaults.AgentOutput)
		if err != nil {
			return nil, err
		}
		next.AgentOutput = choice
	}

	if next == defaults {
		return nil, nil
	}
	bundle.Config.Languages = next

	if err := validateInitLanguageChoice("cli-lang", next.CLI, available); err != nil {
		return nil, err
	}
	if err := validateInitLanguageChoice("tui-lang", next.TUI, available); err != nil {
		return nil, err
	}
	if err := config.SaveBundle(configPath, bundle); err != nil {
		return nil, fmt.Errorf("save %s: %w", configPath, err)
	}
	return map[string]any{
		"cli":          next.CLI,
		"tui":          next.TUI,
		"agent_output": next.AgentOutput,
	}, nil
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
	return domain.NewError(domain.ErrValidation, fmt.Sprintf("--%s %q is not a loaded language code", flag, v), map[string]any{"available": available})
}

// isInteractive reports whether the command should issue TTY prompts.
// True only when (a) the cobra command still writes to os.Stdout
// (tests always SetOut to a buffer, so this turns prompts off in the
// test harness) and (b) stdin is attached to a character device. Both
// gates together mean "real terminal session, not a piped script and
// not a captured-output test run".
func isInteractive(cmd *cobra.Command) bool {
	if cmd.OutOrStdout() != os.Stdout {
		return false
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

// promptLanguageCode prints the available codes and reads one from
// stdin, defaulting to fallback when the user submits an empty line.
// Loops until the entry matches an available code so the seeded
// omakiten.yaml never lands with an invalid value.
func promptLanguageCode(cmd *cobra.Command, label string, available []string, fallback string) (string, error) {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()
	def := fallback
	if def == "" {
		def = "en"
	}
	for {
		fmt.Fprintf(out, "%s [%s] (default %s): ", label, strings.Join(available, ", "), def)
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
		fmt.Fprintf(out, "  unknown code %q — pick one of %s\n", choice, strings.Join(available, ", "))
	}
}

// promptFreeForm reads any text from stdin, defaulting to fallback
// when the user submits an empty line. Used for languages.agent_output
// which is a directive consumed by the agent, not a catalog key.
func promptFreeForm(cmd *cobra.Command, label, fallback string) (string, error) {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()
	def := fallback
	defLabel := def
	if defLabel == "" {
		defLabel = "<none>"
	}
	fmt.Fprintf(out, "%s (default %s): ", label, defLabel)
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

// resolveScopeRoot returns the directory SeedInstall should populate for the
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
			return config.ConfigRootFromYAMLPath(abs), nil
		}
		return paths.ConfigRoot()
	case "local":
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, config.RepoLocalDirName), nil
	default:
		return "", domain.NewError(domain.ErrValidation, "invalid --scope (want global or local)", map[string]any{"scope": scope})
	}
}


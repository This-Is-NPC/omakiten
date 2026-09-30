package cli

import (
	"context"
	"github.com/spf13/cobra"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"strings"
)

func newConfigLanguageCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "language", Short: opts.t("cli.config.language.short"), Long: opts.t("cli.config.language.long")}
	cmd.AddCommand(newConfigLanguageShowCommand(opts), newConfigLanguageSetCommand(opts), newConfigLanguageResetCommand(opts))
	return cmd
}

func newConfigLanguageShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{Use: "show", Short: opts.t("cli.config.language.show.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(context.Context) (any, error) {
			p, err := loadApplicationPreferences()
			if err != nil {
				return nil, err
			}
			languages, err := config.LoadBundledLanguages()
			if err != nil {
				return nil, err
			}
			available := make([]map[string]string, 0, len(languages))
			for _, language := range languages {
				available = append(available, map[string]string{"code": language.Code, "name": language.Name, "native": language.Native, "source": "bundled"})
			}
			path, err := config.PreferencesPath()
			if err != nil {
				return nil, err
			}
			return map[string]any{"path": path, "languages": settingsToMap(p.Languages.Effective()), "available": available, "agent_output_note": opts.t("cli.print.agent_output_note")}, nil
		})
	}}
}

type languageSetInputs struct {
	cli      string
	tui      string
	gui      string
	agent    string
	cliSet   bool
	tuiSet   bool
	guiSet   bool
	agentSet bool
}

func newConfigLanguageSetCommand(opts *runtimeOptions) *cobra.Command {
	var inputs languageSetInputs
	cmd := &cobra.Command{Use: "set", Short: opts.t("cli.config.language.set.short"), Long: opts.t("cli.config.language.set.long"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		inputs.cliSet = cmd.Flags().Changed("cli")
		inputs.tuiSet = cmd.Flags().Changed("tui")
		inputs.guiSet = cmd.Flags().Changed("gui")
		inputs.agentSet = cmd.Flags().Changed("agent")
		return runJSON(cmd, func(context.Context) (any, error) {
			if !inputs.cliSet && !inputs.tuiSet && !inputs.guiSet && !inputs.agentSet {
				return nil, domain.NewError(domain.ErrValidation, opts.t("cli.err.language_set_no_flags"), nil)
			}
			p, err := loadApplicationPreferences()
			if err != nil {
				return nil, err
			}
			p.Languages = languageSettingsFromInputs(p.Languages, inputs)
			return saveApplicationPreferences(p)
		})
	}}
	cmd.Flags().StringVar(&inputs.cli, "cli", "", opts.t("cli.config.language.set.flag.cli"))
	cmd.Flags().StringVar(&inputs.tui, "tui", "", opts.t("cli.config.language.set.flag.tui"))
	cmd.Flags().StringVar(&inputs.gui, "gui", "", opts.t("cli.config.language.set.flag.gui"))
	cmd.Flags().StringVar(&inputs.agent, "agent", "", opts.t("cli.config.language.set.flag.agent"))
	return cmd
}

func languageSettingsFromInputs(settings config.LanguageSettings, inputs languageSetInputs) config.LanguageSettings {
	if inputs.cliSet {
		settings.CLI = strings.TrimSpace(inputs.cli)
	}
	if inputs.tuiSet {
		settings.TUI = strings.TrimSpace(inputs.tui)
	}
	if inputs.guiSet {
		settings.GUI = strings.TrimSpace(inputs.gui)
	}
	if inputs.agentSet {
		settings.AgentOutput = strings.TrimSpace(inputs.agent)
	}
	return settings
}

func newConfigLanguageResetCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{Use: "reset", Short: opts.t("cli.config.language.reset.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(context.Context) (any, error) { return saveApplicationPreferences(config.Preferences{}) })
	}}
}

func loadApplicationPreferences() (config.Preferences, error) {
	p, err := config.LoadPreferences()
	if err != nil {
		path, _ := config.PreferencesPath()
		return p, domain.NewError(domain.ErrConfigInvalid, err.Error(), map[string]any{"path": path, "suggested_command": "okt config language reset"})
	}
	return p, nil
}

func saveApplicationPreferences(p config.Preferences) (any, error) {
	if err := config.ValidatePreferences(p); err != nil {
		return nil, domain.NewError(domain.ErrValidation, err.Error(), nil)
	}
	if err := config.SavePreferences(p); err != nil {
		return nil, err
	}
	path, err := config.PreferencesPath()
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "languages": settingsToMap(p.Languages.Effective())}, nil
}

func settingsToMap(s config.LanguageSettings) map[string]any {
	return map[string]any{"cli": s.CLI, "tui": s.TUI, "gui": s.GUI, "agent_output": s.AgentOutput}
}

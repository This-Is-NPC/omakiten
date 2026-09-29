package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/installer"
)

// configureCommandTree applies defaults without overriding declared argument contracts.
func configureCommandTree(cmd *cobra.Command) {
	if cmd.Runnable() && cmd.Args == nil {
		cmd.Args = cobra.NoArgs
	}
	if cmd.ValidArgsFunction == nil && !strings.HasPrefix(cmd.Use, "validate ") && !strings.HasPrefix(cmd.Use, "diff ") {
		cmd.ValidArgsFunction = cobra.NoFileCompletions
	}
	for name, extensions := range map[string][]string{
		"file": {"md"}, "output": {"md"}, "out": {"db"},
	} {
		if cmd.Flags().Lookup(name) != nil {
			_ = cmd.MarkFlagFilename(name, extensions...)
		}
	}
	if cmd.Flags().Lookup("root") != nil {
		_ = cmd.MarkFlagDirname("root")
	}
	configureFlagCompletions(cmd)
	if cmd.RunE != nil {
		run := cmd.RunE
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if err := validateCommandFlags(cmd); err != nil {
				return writeError(cmd, err)
			}
			return run(cmd, args)
		}
	}
	for _, child := range cmd.Commands() {
		configureCommandTree(child)
	}
}

func validateCommandFlags(cmd *cobra.Command) error {
	var invalid error
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if invalid == nil {
			invalid = validateFlagValue(flag)
		}
	})
	if invalid != nil {
		return invalid
	}
	if cmd.Flags().Lookup("no-edit") != nil {
		noEdit, _ := cmd.Flags().GetBool("no-edit")
		if !noEdit && !stdinIsTTY() && strings.TrimSpace(os.Getenv("EDITOR")) == "" && strings.TrimSpace(os.Getenv("VISUAL")) == "" {
			return domain.NewError(domain.ErrValidation, t("cli.err.editor_headless"), nil)
		}
	}
	return nil
}

func validateFlagValue(flag *pflag.Flag) error {
	field := "--" + flag.Name
	value := flag.Value.String()
	required := len(flag.Annotations[cobra.BashCompOneRequiredFlag]) != 0
	switch flag.Value.Type() {
	case "int", "int64":
		number, _ := strconv.ParseInt(value, 10, 64)
		positive := required || flag.Name == "project-id"
		if number < 0 || (positive && number == 0) {
			key := "cli.err.number_nonnegative_fmt"
			if positive {
				key = "cli.err.id_positive_fmt"
			}
			return domain.NewError(domain.ErrValidation, fmt.Sprintf(t(key), field), map[string]any{"value": value})
		}
	case "string":
		if strings.TrimSpace(value) == "" && (required || isPathFlag(flag.Name)) {
			return domain.NewError(domain.ErrValidation, field+" must not be empty", nil)
		}
	}
	return nil
}

func isPathFlag(name string) bool {
	switch name {
	case "config", "db", "root", "file", "output", "out":
		return true
	default:
		return false
	}
}

func configureFlagCompletions(cmd *cobra.Command) {
	choices := map[string][]string{"category": knownCategoryNames(), "harnesses": installer.SupportedHarnesses()}
	for _, preset := range config.ListPresets() {
		choices["preset"] = append(choices["preset"], preset.Name)
	}
	if parent := cmd.Parent(); parent != nil {
		switch parent.Name() {
		case "config":
			choices["scope"] = []string{"global", "local"}
		case "comment":
			choices["scope"] = []string{domain.CommentScopeTask, domain.CommentScopeProject, domain.CommentScopeUniversal}
		case "law":
			choices["scope"] = []string{string(domain.LawScopeGlobal), string(domain.LawScopeProject), string(domain.LawScopePersona)}
		}
	}
	for name, values := range choices {
		if cmd.Flags().Lookup(name) != nil {
			_ = cmd.RegisterFlagCompletionFunc(name, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp))
		}
	}
}

func streamIsTTY(stream any) bool {
	file, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(file.Fd()))
}

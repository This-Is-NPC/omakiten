package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
	"omakiten/internal/output"
)

// Execute runs a command and reports terminal errors on stderr.
// Commands that already emitted a JSON failure retain their exit status.
func Execute(root *cobra.Command) int {
	cmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}
	if code, ok := ExitCode(err); ok {
		return code
	}
	if cmd == nil {
		cmd = root
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) && cmd.Name() != "tui" {
		err = domain.NewError(domain.ErrValidation, err.Error(), nil)
	}
	if cmd.Name() == "tui" {
		printCommandFailure(cmd.ErrOrStderr(), commandFailure(cmd, err))
	} else {
		_ = writeError(cmd, err)
	}
	return 1
}

func failureGuidance(cmd *cobra.Command, code string, original map[string]any) map[string]any {
	details := make(map[string]any, len(original)+2)
	for key, value := range original {
		details[key] = value
	}
	details["help_command"] = cmd.CommandPath() + " --help"
	details["usage"] = cmd.UseLine()
	if _, exists := details["suggested_command"]; !exists {
		if path := recoveryCommand(domain.ErrorCode(code)); path != "" {
			details["suggested_command"] = scopedCommand(cmd, path)
		}
	}
	return details
}

func recoveryCommand(code domain.ErrorCode) string {
	switch code {
	case domain.ErrProjectNotFound, domain.ErrProjectAmbiguous:
		return "projects list"
	case domain.ErrTaskNotFound:
		return "list"
	case domain.ErrBucketNotFound, domain.ErrWorkflowInvalidTransition:
		return "workflow show"
	case domain.ErrLawNotFound:
		return "law list"
	case domain.ErrSkillNotFound, domain.ErrSkillReferenced:
		return "skill list"
	case domain.ErrPersonaNotFound:
		return "persona list"
	case domain.ErrPlanNotFound, domain.ErrPlanWaveNotFound:
		return "plan list"
	case domain.ErrTagNotFound, domain.ErrTagConflict:
		return "tag list-all"
	case domain.ErrSolutionNotFound:
		return "solution list-top"
	case domain.ErrSearchIndexInvalid:
		return "db reindex"
	default:
		return ""
	}
}

func scopedCommand(cmd *cobra.Command, path string) string {
	parts := []string{cmd.Root().Name()}
	for _, name := range []string{"db", "config", "project", "project-id"} {
		if strings.HasPrefix(path, "projects ") && (name == "project" || name == "project-id") {
			continue
		}
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed {
			parts = append(parts, "--"+name, shellQuoteArg(flag.Value.String()))
		}
	}
	return strings.Join(parts, " ") + " " + path
}

func printCommandFailure(w io.Writer, envelope output.Envelope) {
	fmt.Fprintf(w, "%s: %s\n", envelope.Code, envelope.Message)
	seen := make(map[string]bool)
	printDetails := func(details map[string]any) {
		for _, key := range []string{"path", "message", "error", "hint", "repair_command", "suggested_command", "allowed", "available"} {
			if value := details[key]; value != nil {
				text := fmt.Sprint(value)
				if text != "" && !seen[text] {
					fmt.Fprintln(w, text)
					seen[text] = true
				}
			}
		}
	}
	printDetails(envelope.Details)
	entries, _ := envelope.Details["errors"].([]map[string]any)
	for _, entry := range entries {
		printDetails(entry)
	}
	fmt.Fprintf(w, "\n%s\n", envelope.Details["help_command"])
}

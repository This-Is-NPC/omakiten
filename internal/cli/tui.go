package cli

import (
	"github.com/spf13/cobra"
)

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeSuccess(cmd, map[string]any{"status": "not_implemented", "msg": "TUI skeleton is reserved for the next implementation slice"})
		},
	}
}

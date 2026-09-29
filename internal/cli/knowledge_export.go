package cli

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"omakiten/internal/contract"
)

func newKnowledgeExportCLICommand() *cobra.Command {
	return &cobra.Command{
		Use: "export-cli", Short: "Export the live CLI command tree as a knowledge inventory",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(buildCLIInventory(cmd.Root()))
		},
	}
}

func buildCLIInventory(root *cobra.Command) contract.CLIInventory {
	inventory := contract.CLIInventory{Version: 1}
	var visit func(*cobra.Command, string, string)
	visit = func(command *cobra.Command, id, parent string) {
		if command.Hidden {
			return
		}
		item := contract.CLICommand{
			ID: id, Parent: parent, Name: command.CommandPath(),
			Summary: command.Short, Description: command.Long,
		}
		command.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden {
				item.Flags = append(item.Flags, contract.CLIFlag{Name: flag.Name, Shorthand: flag.Shorthand, Description: flag.Usage})
			}
		})
		inventory.Commands = append(inventory.Commands, item)
		for _, child := range command.Commands() {
			childID := child.Name()
			if parent != "" {
				childID = id + "." + childID
			}
			visit(child, childID, id)
		}
	}
	visit(root, strings.Fields(root.Use)[0], "")
	return inventory
}

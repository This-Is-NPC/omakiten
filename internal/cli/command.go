package cli

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func newCommandCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "command", Short: opts.t("cli.command.short")}
	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: opts.t("cli.command.list.short"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ListCommands(ctx)
			})
		},
	})
	var arguments string
	resolve := &cobra.Command{
		Use: "resolve NAME", Short: opts.t("cli.command.resolve.short"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				input := contract.ResolveCommandInput{Name: args[0]}
				if arguments != "" {
					if err := json.Unmarshal([]byte(arguments), &input.Arguments); err != nil || input.Arguments == nil {
						return nil, domain.NewError(domain.ErrValidation, "arguments must be a JSON object", nil)
					}
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ResolveCommand(ctx, input)
			})
		},
	}
	resolve.Flags().StringVar(&arguments, "arguments", "", opts.t("cli.command.resolve.flag.arguments"))
	cmd.AddCommand(resolve)
	return cmd
}

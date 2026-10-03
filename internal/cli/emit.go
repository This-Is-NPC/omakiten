package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// newEmitCommand emits an external event the project's kit declares, for
// scripts, CI jobs, and Git hooks: okt emit ci_failed --field branch=main.
func newEmitCommand(opts *runtimeOptions) *cobra.Command {
	var fields []string
	cmd := &cobra.Command{
		Use:   "emit NAME",
		Short: opts.t("cli.emit.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				payload := make(map[string]string, len(fields))
				for _, field := range fields {
					key, value, ok := strings.Cut(field, "=")
					if !ok {
						return nil, domain.NewError(domain.ErrValidation, fmt.Sprintf(opts.t("cli.err.emit_field"), field), map[string]any{"field": field})
					}
					payload[key] = value
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().EmitEvent(ctx, contract.EmitEventInput{
					ProjectSelector: opts.projectSelector(),
					Name:            args[0],
					Payload:         payload,
				})
			})
		},
	}
	cmd.Flags().StringArrayVar(&fields, "field", nil, opts.t("cli.emit.flag.field"))
	return cmd
}

package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func newSearchCommand(opts *runtimeOptions) *cobra.Command {
	var entityTypes []string
	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: opts.t("cli.search.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().Search(ctx, operation.SearchInput{
					ProjectSelector: operation.ProjectSelector{
						Project:   opts.project,
						ProjectID: opts.projectID,
					},
					Query:       args[0],
					EntityTypes: entityTypes,
				})
			})
		},
	}
	cmd.Flags().StringSliceVar(&entityTypes, "entity-type", nil, opts.t("cli.search.flag.entity-type"))
	return cmd
}

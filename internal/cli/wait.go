package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newWaitCommand(opts *runtimeOptions) *cobra.Command {
	var input contract.WaitInput
	cmd := &cobra.Command{
		Use:   "wait --task ID --tag GLOB [--timeout 10m] [--after EVENT_ID]",
		Short: opts.t("cli.wait.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				input.ProjectSelector = opts.projectSelector()
				return rt.operationService().Wait(ctx, input)
			})
		},
	}
	cmd.Flags().Int64Var(&input.TaskID, "task", 0, opts.t("cli.wait.flag.task"))
	cmd.Flags().StringVar(&input.Tag, "tag", "", opts.t("cli.wait.flag.tag"))
	cmd.Flags().StringVar(&input.Timeout, "timeout", "", opts.t("cli.wait.flag.timeout"))
	cmd.Flags().Int64Var(&input.AfterID, "after", 0, opts.t("cli.wait.flag.after"))
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("tag")
	return cmd
}

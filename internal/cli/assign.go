package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

// newAssignCommand wires `okt assign TASK_ID [WHO]`. Passing an empty WHO
// (or omitting it) clears tasks.assigned_to to NULL — the documented
// recovery path for tasks whose claiming agent crashed without finishing.
// Listed at the top level (rather than under a `task` parent) to match
// the existing flat surface (`okt move`, `okt edit`, `okt archive`, ...).
// Distinct from `okt plan assign`, which attaches a task to a plan wave
// (census slug plan.task.assign). This command is task.assign / assignee.
func newAssignCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign TASK_ID [WHO]",
		Short: opts.t("cli.task.assign.short"),
		Long:  opts.t("cli.task.assign.long"),
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}
				who := ""
				if len(args) == 2 {
					who = args[1]
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				return rt.operationService().AssignTask(ctx, contract.AssignTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					Assignee:        who,
				})
			})
		},
	}
	return cmd
}

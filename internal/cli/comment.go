package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func newCommentCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Manage task comments",
	}

	var body string
	var author string
	add := &cobra.Command{
		Use:   "add TASK_ID",
		Short: "Add a task comment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}
			rt, err := opts.open(ctx, true)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rt.store.Close() }()
			ctx = rt.WithActivityRepo(ctx)

			project, err := opts.resolveProject(ctx, rt.store)
			if err != nil {
				return nil, err
			}
			comment, err := app.NewCommentService(rt.store).Add(ctx, project, taskID, body, author, nil)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comment": comment}, nil
			})
		},
	}
	add.Flags().StringVarP(&body, "body", "b", "", "comment body")
	add.Flags().StringVarP(&author, "author", "a", "human", "author type: human or agent")
	_ = add.MarkFlagRequired("body")

	list := &cobra.Command{
		Use:   "list TASK_ID",
		Short: "List task comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer func() { _ = rt.store.Close() }()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				comments, err := app.NewCommentService(rt.store).List(ctx, project, taskID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comments": comments}, nil
			})
		},
	}

	cmd.AddCommand(add)
	cmd.AddCommand(list)
	return cmd
}

func parseTaskID(value string) (int64, error) {
	taskID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, domain.NewError(domain.ErrValidation, "task id must be numeric", map[string]any{"value": value})
	}
	return taskID, nil
}

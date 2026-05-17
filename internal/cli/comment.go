package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
)

func newCommentCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: opts.t("cli.comment.short"),
	}

	var body string
	var author string
	var tags []string
	add := &cobra.Command{
		Use:   "add TASK_ID",
		Short: opts.t("cli.comment.add.short"),
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
			defer rt.close()
			ctx = rt.WithActivityRepo(ctx)

			project, err := opts.resolveProject(ctx, rt.store)
			if err != nil {
				return nil, err
			}
			comment, err := rt.commentService().Add(ctx, project, taskID, body, author, tags)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comment": comment}, nil
			})
		},
	}
	add.Flags().StringVarP(&body, "body", "b", "", opts.t("cli.comment.add.flag.body"))
	add.Flags().StringVarP(&author, "author", "a", "human", opts.t("cli.comment.add.flag.author"))
	add.Flags().StringArrayVarP(&tags, "tag", "T", nil, opts.t("cli.comment.add.flag.tag"))
	_ = add.MarkFlagRequired("body")

	list := &cobra.Command{
		Use:   "list TASK_ID",
		Short: opts.t("cli.comment.list.short"),
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
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				comments, err := rt.commentService().List(ctx, project, taskID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comments": comments}, nil
			})
		},
	}

	var editBody string
	var editTags []string
	edit := &cobra.Command{
		Use:   "edit COMMENT_ID",
		Short: opts.t("cli.comment.edit.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				commentID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				workflow := rt.activeWorkflow()
				comment, err := rt.commentServiceWithWorkflow(workflow).Edit(ctx, project, commentID, editBody, editTags)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comment": comment}, nil
			})
		},
	}
	edit.Flags().StringVarP(&editBody, "body", "b", "", opts.t("cli.comment.edit.flag.body"))
	edit.Flags().StringArrayVarP(&editTags, "tag", "T", nil, opts.t("cli.comment.edit.flag.tag"))
	_ = edit.MarkFlagRequired("body")

	var deleteConfirmed bool
	del := &cobra.Command{
		Use:   "delete COMMENT_ID",
		Short: opts.t("cli.comment.delete.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				commentID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}
				if !deleteConfirmed {
					return nil, domain.NewError(domain.ErrValidation, opts.t("cli.err.comment_delete_requires_confirm"), map[string]any{"comment_id": commentID})
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				workflow := rt.activeWorkflow()
				event, err := rt.commentServiceWithWorkflow(workflow).Remove(ctx, project, commentID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "snapshot": event}, nil
			})
		},
	}
	del.Flags().BoolVar(&deleteConfirmed, "confirm", false, opts.t("cli.comment.delete.flag.confirm"))

	cmd.AddCommand(add)
	cmd.AddCommand(list)
	cmd.AddCommand(edit)
	cmd.AddCommand(del)
	return cmd
}

func parseTaskID(value string) (int64, error) {
	taskID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, domain.NewError(domain.ErrValidation, t("cli.err.task_id_not_numeric"), map[string]any{"value": value})
	}
	return taskID, nil
}

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
	var tags []string
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
	add.Flags().StringVarP(&body, "body", "b", "", "comment body")
	add.Flags().StringVarP(&author, "author", "a", "human", "author type: human or agent")
	add.Flags().StringArrayVarP(&tags, "tag", "T", nil, "tag name (repeatable)")
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
		Short: "Edit a comment body and replace its tags (subject to bucket policy)",
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
				workflow := app.NewWorkflowServiceFromStore(rt.store, rt.activeRegistry())
				comment, err := rt.commentServiceWithWorkflow(workflow).Edit(ctx, project, commentID, editBody, editTags)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comment": comment}, nil
			})
		},
	}
	edit.Flags().StringVarP(&editBody, "body", "b", "", "new comment body")
	edit.Flags().StringArrayVarP(&editTags, "tag", "T", nil, "tag name (repeatable; replaces all existing tags)")
	_ = edit.MarkFlagRequired("body")

	var deleteConfirmed bool
	del := &cobra.Command{
		Use:   "delete COMMENT_ID",
		Short: "Hard-delete a comment (subject to bucket policy)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				commentID, err := parseTaskID(args[0])
				if err != nil {
					return nil, err
				}
				if !deleteConfirmed {
					return nil, domain.NewError(domain.ErrValidation, "comment delete requires --confirm to acknowledge the destructive operation", map[string]any{"comment_id": commentID})
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
				workflow := app.NewWorkflowServiceFromStore(rt.store, rt.activeRegistry())
				event, err := rt.commentServiceWithWorkflow(workflow).Remove(ctx, project, commentID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "snapshot": event}, nil
			})
		},
	}
	del.Flags().BoolVar(&deleteConfirmed, "confirm", false, "required: confirm the destructive delete")

	cmd.AddCommand(add)
	cmd.AddCommand(list)
	cmd.AddCommand(edit)
	cmd.AddCommand(del)
	return cmd
}

func parseTaskID(value string) (int64, error) {
	taskID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, domain.NewError(domain.ErrValidation, "task id must be numeric", map[string]any{"value": value})
	}
	return taskID, nil
}

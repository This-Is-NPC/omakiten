package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func newCommentCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: opts.t("cli.comment.short"),
	}

	cmd.AddCommand(newCommentAddCommand(opts))
	cmd.AddCommand(newCommentListCommand(opts))
	cmd.AddCommand(newCommentEditCommand(opts))
	cmd.AddCommand(newCommentDeleteCommand(opts))
	return cmd
}

// newCommentAddCommand wires `okt comment add [TASK_ID]` through
// operation.Service.AddComment. Task scope requires the TASK_ID arg;
// project/universal scopes must not carry one (enforced by the facade).
func newCommentAddCommand(opts *runtimeOptions) *cobra.Command {
	var (
		body   string
		author string
		tags   []string
		scope  string
		kind   string
		title  string
		pinned bool
	)
	add := &cobra.Command{
		Use:   "add [TASK_ID]",
		Short: opts.t("cli.comment.add.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				var taskID int64
				if len(args) == 1 {
					parsed, err := parseTaskID(args[0])
					if err != nil {
						return nil, err
					}
					taskID = parsed
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().AddComment(ctx, contract.AddCommentInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					Scope:           scope,
					Body:            body,
					Title:           title,
					Kind:            kind,
					Pinned:          pinned,
					AuthorType:      author,
					Tags:            tags,
				})
			})
		},
	}
	add.Flags().StringVarP(&body, "body", "b", "", opts.t("cli.comment.add.flag.body"))
	add.Flags().StringVarP(&author, "author", "a", "human", opts.t("cli.comment.add.flag.author"))
	add.Flags().StringArrayVarP(&tags, "tag", "T", nil, opts.t("cli.comment.add.flag.tag"))
	add.Flags().StringVar(&scope, "scope", "", opts.t("cli.comment.add.flag.scope"))
	add.Flags().StringVar(&kind, "kind", "", opts.t("cli.comment.add.flag.kind"))
	add.Flags().StringVar(&title, "title", "", opts.t("cli.comment.add.flag.title"))
	add.Flags().BoolVar(&pinned, "pinned", false, opts.t("cli.comment.add.flag.pinned"))
	_ = add.MarkFlagRequired("body")
	return add
}

// newCommentListCommand wires `okt comment list [TASK_ID]` through
// operation.Service.ListComments, preserving scopes, tags, pinned, --since,
// --query, and --comment-id filters.
func newCommentListCommand(opts *runtimeOptions) *cobra.Command {
	var (
		scope     string
		kind      string
		tag       string
		query     string
		since     string
		pinned    bool
		commentID int64
	)
	list := &cobra.Command{
		Use:   "list [TASK_ID]",
		Short: opts.t("cli.comment.list.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				var taskID int64
				if len(args) == 1 {
					parsed, err := parseTaskID(args[0])
					if err != nil {
						return nil, err
					}
					taskID = parsed
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ListComments(ctx, contract.ListCommentsInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					CommentID:       commentID,
					Scope:           scope,
					Kind:            kind,
					Tag:             tag,
					Pinned:          pinned,
					Query:           query,
					Since:           since,
				})
			})
		},
	}
	list.Flags().StringVar(&scope, "scope", "", opts.t("cli.comment.list.flag.scope"))
	list.Flags().StringVar(&kind, "kind", "", opts.t("cli.comment.list.flag.kind"))
	list.Flags().StringVarP(&tag, "tag", "T", "", opts.t("cli.comment.list.flag.tag"))
	list.Flags().StringVar(&query, "query", "", opts.t("cli.comment.list.flag.query"))
	list.Flags().StringVar(&since, "since", "", opts.t("cli.comment.list.flag.since"))
	list.Flags().BoolVar(&pinned, "pinned", false, opts.t("cli.comment.list.flag.pinned"))
	list.Flags().Int64Var(&commentID, "comment-id", 0, opts.t("cli.comment.list.flag.comment_id"))
	return list
}

// newCommentEditCommand wires `okt comment edit COMMENT_ID` through
// operation.Service.EditComment with tri-state Body/Title/Kind/Pinned/Tags:
// a field is only forwarded when its flag was explicitly set.
func newCommentEditCommand(opts *runtimeOptions) *cobra.Command {
	var (
		editBody string
		editTags []string
		title    string
		kind     string
		pinned   bool
	)
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

				return rt.operationService().EditComment(ctx, commentEditInput(
					cmd, opts, commentID, editBody, editTags, title, kind, pinned,
				))
			})
		},
	}
	edit.Flags().StringVarP(&editBody, "body", "b", "", opts.t("cli.comment.edit.flag.body"))
	edit.Flags().StringArrayVarP(&editTags, "tag", "T", nil, opts.t("cli.comment.edit.flag.tag"))
	edit.Flags().StringVar(&title, "title", "", opts.t("cli.comment.edit.flag.title"))
	edit.Flags().StringVar(&kind, "kind", "", opts.t("cli.comment.edit.flag.kind"))
	edit.Flags().BoolVar(&pinned, "pinned", false, opts.t("cli.comment.edit.flag.pinned"))
	return edit
}

func commentEditInput(cmd *cobra.Command, opts *runtimeOptions, commentID int64, body string, tags []string, title, kind string, pinned bool) contract.EditCommentInput {
	input := contract.EditCommentInput{
		ProjectSelector: opts.projectSelector(),
		CommentID:       commentID,
	}
	if cmd.Flags().Changed("body") {
		input.Body = &body
	}
	if cmd.Flags().Changed("title") {
		input.Title = &title
	}
	if cmd.Flags().Changed("kind") {
		input.Kind = &kind
	}
	if cmd.Flags().Changed("pinned") {
		input.Pinned = &pinned
	}
	// Tags is tri-state: --tag unset leaves Tags nil so the store preserves
	// existing tags; --tag given (even empty) replaces.
	if cmd.Flags().Changed("tag") {
		input.Tags = tags
	}
	return input
}

// newCommentDeleteCommand wires `okt comment delete COMMENT_ID [--confirm]`
// through operation.Service.DeleteComment. Without --confirm the facade
// returns a Confirmation block; with --confirm the hard delete runs.
func newCommentDeleteCommand(opts *runtimeOptions) *cobra.Command {
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
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().DeleteComment(ctx, contract.DeleteCommentInput{
					ProjectSelector: opts.projectSelector(),
					CommentID:       commentID,
					Confirmed:       deleteConfirmed,
				})
			})
		},
	}
	del.Flags().BoolVar(&deleteConfirmed, "confirm", false, opts.t("cli.comment.delete.flag.confirm"))
	return del
}

func parseTaskID(value string) (int64, error) {
	taskID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, domain.NewError(domain.ErrValidation, t("cli.err.task_id_not_numeric"), map[string]any{"value": value})
	}
	return taskID, nil
}

package cli

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
)

// commentSinceLayout mirrors agent.commentSinceLayout: the SQLite datetime
// shape the events table stamps via CURRENT_TIMESTAMP. The `--since` window
// floor is formatted with this layout so CommentFilter.CreatedAfter compares
// lexicographically against created_at.
const commentSinceLayout = "2006-01-02 15:04:05"

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

// newCommentAddCommand wires `okt comment add [TASK_ID]` to the scope-aware
// AddScoped service method, mirroring the agent comments.add handler: task
// scope requires the TASK_ID arg; project/universal scopes must not carry one.
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
				resolvedScope := strings.TrimSpace(scope)
				if resolvedScope == "" {
					resolvedScope = domain.CommentScopeTask
				}

				var taskID int64
				hasTaskArg := len(args) == 1
				if hasTaskArg {
					parsed, err := parseTaskID(args[0])
					if err != nil {
						return nil, err
					}
					taskID = parsed
				}

				switch resolvedScope {
				case domain.CommentScopeTask:
					if !hasTaskArg {
						return nil, domain.NewError(domain.ErrValidation,
							opts.t("cli.err.comment_task_scope_requires_id"), map[string]any{"scope": resolvedScope})
					}
				case domain.CommentScopeProject, domain.CommentScopeUniversal:
					if hasTaskArg {
						return nil, domain.NewError(domain.ErrValidation,
							opts.t("cli.err.comment_scope_no_task_id"), map[string]any{"scope": resolvedScope, "task_id": taskID})
					}
				default:
					return nil, domain.NewError(domain.ErrValidation,
						opts.t("cli.err.comment_unknown_scope"), map[string]any{"scope": resolvedScope})
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

				domainTags := make([]domain.Tag, 0, len(tags))
				for _, raw := range tags {
					domainTags = append(domainTags, domain.Tag{Name: raw, Label: raw})
				}
				comment, err := rt.commentService().AddScoped(ctx, project, domain.CommentWrite{
					Scope:      resolvedScope,
					TaskID:     taskID,
					Body:       body,
					Title:      strings.TrimSpace(title),
					Kind:       strings.TrimSpace(kind),
					Pinned:     pinned,
					AuthorType: author,
					Tags:       domainTags,
				})
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
	add.Flags().StringVar(&scope, "scope", "", opts.t("cli.comment.add.flag.scope"))
	add.Flags().StringVar(&kind, "kind", "", opts.t("cli.comment.add.flag.kind"))
	add.Flags().StringVar(&title, "title", "", opts.t("cli.comment.add.flag.title"))
	add.Flags().BoolVar(&pinned, "pinned", false, opts.t("cli.comment.add.flag.pinned"))
	_ = add.MarkFlagRequired("body")
	return add
}

// newCommentListCommand wires `okt comment list [TASK_ID]` to either the
// filterable Query surface (when any scoped flag is set) or the legacy
// task-scoped List (a bare TASK_ID with no other filter), mirroring the agent
// comments.list handler's routing.
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

				resolvedScope := strings.TrimSpace(scope)
				resolvedKind := strings.TrimSpace(kind)
				resolvedTag := strings.TrimSpace(tag)
				resolvedQuery := strings.TrimSpace(query)
				resolvedSince := strings.TrimSpace(since)

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

				// Pure task-scoped listing (no extra filters) keeps the
				// original List path so default behaviour is unchanged.
				if resolvedScope == "" && resolvedKind == "" && resolvedTag == "" &&
					resolvedQuery == "" && resolvedSince == "" && !pinned && commentID <= 0 {
					comments, err := rt.commentService().List(ctx, project, taskID)
					if err != nil {
						return nil, err
					}
					return map[string]any{"project": project, "comments": comments}, nil
				}

				// Universal comments carry project_id NULL and only match
				// when ProjectID is 0. A comment_id names a globally unique
				// row but keeps the caller's project id so it cannot read
				// another project's task/project comment; the store's id path
				// still lets project-less universal rows fall through.
				projectID := project.ID
				if resolvedScope == domain.CommentScopeUniversal {
					projectID = 0
				}
				filter := domain.CommentFilter{
					CommentID:  commentID,
					Scope:      resolvedScope,
					ProjectID:  projectID,
					TaskID:     taskID,
					Kind:       resolvedKind,
					Tag:        resolvedTag,
					PinnedOnly: pinned,
					Search:     resolvedQuery,
				}
				if resolvedSince != "" {
					floor, err := resolveLogSince(resolvedSince, rt.activeSnapshot(), time.Now)
					if err != nil {
						return nil, err
					}
					if !floor.IsZero() {
						filter.CreatedAfter = floor.UTC().Format(commentSinceLayout)
					}
				}
				comments, err := rt.commentService().Query(ctx, project, filter)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comments": comments}, nil
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

// newCommentEditCommand wires `okt comment edit COMMENT_ID` to the scoped
// EditScoped path with tri-state Title/Kind/Pinned: a field is only forwarded
// when its flag was explicitly set, so a body-only (or metadata-only) edit
// never wipes a pinned flag, title, or kind (the #385 fix end-to-end).
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

				bodyChanged := cmd.Flags().Changed("body")
				titleChanged := cmd.Flags().Changed("title")
				kindChanged := cmd.Flags().Changed("kind")
				pinnedChanged := cmd.Flags().Changed("pinned")
				tagChanged := cmd.Flags().Changed("tag")
				if !bodyChanged && !titleChanged && !kindChanged && !pinnedChanged && !tagChanged {
					return nil, domain.NewError(domain.ErrValidation,
						opts.t("cli.err.comment_edit_requires_field"), map[string]any{"comment_id": commentID})
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

				// Body is tri-state: omit --body to leave the stored body
				// untouched (a pure metadata edit), so we pass nil and let the
				// store preserve the previous body. No read-modify-write here.
				var cEdit domain.CommentEdit
				if bodyChanged {
					cEdit.Body = &editBody
				}
				if titleChanged {
					trimmed := strings.TrimSpace(title)
					cEdit.Title = &trimmed
				}
				if kindChanged {
					trimmed := strings.TrimSpace(kind)
					cEdit.Kind = &trimmed
				}
				if pinnedChanged {
					cEdit.Pinned = &pinned
				}

				var rawTags []string
				if tagChanged {
					rawTags = editTags
				}

				workflow := rt.activeWorkflow()
				comment, err := rt.commentServiceWithWorkflow(workflow).EditScoped(ctx, project, commentID, cEdit, rawTags)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "comment": comment}, nil
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
	return del
}

func parseTaskID(value string) (int64, error) {
	taskID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, domain.NewError(domain.ErrValidation, t("cli.err.task_id_not_numeric"), map[string]any{"value": value})
	}
	return taskID, nil
}

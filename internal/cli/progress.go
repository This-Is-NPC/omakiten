package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func newProgressCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "progress",
		Short: opts.t("cli.progress.short"),
	}
	cmd.AddCommand(newProgressRecordCommand(opts))
	return cmd
}

func newProgressRecordCommand(opts *runtimeOptions) *cobra.Command {
	var (
		taskID       int64
		title        string
		description  string
		priority     string
		moveToBucket string
		comment      string
		authorType   string
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: opts.t("cli.progress.record.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				input := operation.RecordProgressInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
					MoveToBucket:    moveToBucket,
					Comment:         comment,
					AuthorType:      authorType,
				}
				if cmd.Flags().Changed("title") {
					input.Title = &title
				}
				if cmd.Flags().Changed("description") {
					input.Description = &description
				}
				if cmd.Flags().Changed("priority") {
					input.Priority = &priority
				}
				return rt.operationService().RecordProgress(ctx, input)
			})
		},
	}
	cmd.Flags().Int64Var(&taskID, "task-id", 0, opts.t("cli.progress.record.flag.task-id"))
	cmd.Flags().StringVar(&title, "title", "", opts.t("cli.progress.record.flag.title"))
	cmd.Flags().StringVar(&description, "description", "", opts.t("cli.progress.record.flag.description"))
	cmd.Flags().StringVar(&priority, "priority", "", opts.t("cli.progress.record.flag.priority"))
	cmd.Flags().StringVar(&moveToBucket, "move-to-bucket", "", opts.t("cli.progress.record.flag.move-to-bucket"))
	cmd.Flags().StringVar(&comment, "comment", "", opts.t("cli.progress.record.flag.comment"))
	cmd.Flags().StringVar(&authorType, "author-type", "", opts.t("cli.progress.record.flag.author-type"))
	_ = cmd.MarkFlagRequired("task-id")
	return cmd
}

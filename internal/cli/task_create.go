package cli

import (
	"bytes"
	"context"

	"github.com/spf13/cobra"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/workfile"
)

func newTaskCreateCommand(opts *runtimeOptions) *cobra.Command {
	var title, description, priority, bucket, template, file string
	var parent int64
	var confirmed, dryRun bool
	cmd := &cobra.Command{Use: "create", Short: opts.t("cli.task.create.short"), Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(ctx context.Context) (any, error) {
			input := contract.CreateTaskInput{ProjectSelector: opts.projectSelector(), Title: title, Description: description, Priority: priority, BucketKey: bucket, TemplateSlug: template, Confirmed: confirmed}
			if cmd.Flags().Changed("parent") {
				input.ParentID = &parent
			}
			return runTaskCreate(ctx, cmd, opts, input, file, dryRun)

		})
	}
	cmd.Flags().StringVarP(&title, "title", "t", "", opts.t("cli.task.create.flag.title"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.task.create.flag.description"))
	cmd.Flags().StringVar(&priority, "priority", "", opts.t("cli.task.create.flag.priority"))
	cmd.Flags().StringVarP(&bucket, "bucket", "b", "", opts.t("cli.task.create.flag.bucket"))
	cmd.Flags().StringVar(&template, "template", "", opts.t("cli.task.create.flag.template"))
	cmd.Flags().Int64Var(&parent, "parent", 0, opts.t("cli.task.create.flag.parent"))
	cmd.Flags().BoolVar(&confirmed, "confirm", false, opts.t("cli.work.flag.confirm"))
	cmd.Flags().StringVar(&file, "file", "", opts.t("cli.work.flag.file"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, opts.t("cli.work.flag.dry_run"))
	cmd.MarkFlagsMutuallyExclusive("file", "description")
	return cmd
}

func runTaskCreate(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, input contract.CreateTaskInput, file string, dryRun bool) (any, error) {
	if file != "" {
		data, err := readWorkFile(cmd, file)
		if err != nil {
			return nil, err
		}
		firstLine, _, _ := bytes.Cut(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), []byte("\n"))
		if bytes.Equal(bytes.TrimSuffix(firstLine, []byte("\r")), []byte("---")) {
			if err := validateStructuredTaskFlags(cmd); err != nil {
				return nil, err
			}
			doc, err := workfile.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, domain.NewError(domain.ErrValidation, err.Error(), nil)
			}
			return importWorkDocument(ctx, opts, "task", doc, dryRun, input.Confirmed)
		}
		input.Description = string(data)
	}
	if dryRun {
		return nil, domain.NewError(domain.ErrValidation, "--dry-run requires a structured OKF file", nil)
	}
	rt, err := opts.open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer rt.close()
	return rt.operationService().CreateTaskIntent(ctx, input)
}

func validateStructuredTaskFlags(cmd *cobra.Command) error {
	for _, flag := range []string{"title", "priority", "bucket", "parent", "template"} {
		if cmd.Flags().Changed(flag) {
			return domain.NewError(domain.ErrValidation, "structured files supply their own --"+flag+" value", nil)
		}
	}
	return nil
}

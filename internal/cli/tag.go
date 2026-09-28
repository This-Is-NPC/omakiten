package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func newTagCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: opts.t("cli.tag.short"),
	}
	cmd.AddCommand(newTagAddCommand(opts))
	cmd.AddCommand(newTagListCommand(opts))
	cmd.AddCommand(newTagListAllCommand(opts))
	cmd.AddCommand(newTagRemoveCommand(opts))
	cmd.AddCommand(newTagMergeCommand(opts))
	return cmd
}

func newTagAddCommand(opts *runtimeOptions) *cobra.Command {
	var entityType, tagName string
	var entityID int64
	cmd := &cobra.Command{
		Use:   "add",
		Short: opts.t("cli.tag.add.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().AddTag(ctx, operation.AddTagInput{
					ProjectSelector: operation.ProjectSelector{
						Project:   opts.project,
						ProjectID: opts.projectID,
					},
					EntityType: entityType,
					EntityID:   entityID,
					TagName:    tagName,
				})
			})
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", opts.t("cli.tag.flag.entity-type"))
	cmd.Flags().Int64Var(&entityID, "entity-id", 0, opts.t("cli.tag.flag.entity-id"))
	cmd.Flags().StringVar(&tagName, "name", "", opts.t("cli.tag.flag.name"))
	_ = cmd.MarkFlagRequired("entity-type")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newTagListCommand(opts *runtimeOptions) *cobra.Command {
	var entityType string
	var entityID int64
	cmd := &cobra.Command{
		Use:   "list",
		Short: opts.t("cli.tag.list.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ListTags(ctx, operation.ListTagsInput{
					ProjectSelector: operation.ProjectSelector{
						Project:   opts.project,
						ProjectID: opts.projectID,
					},
					EntityType: entityType,
					EntityID:   entityID,
				})
			})
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", opts.t("cli.tag.flag.entity-type"))
	cmd.Flags().Int64Var(&entityID, "entity-id", 0, opts.t("cli.tag.flag.entity-id"))
	_ = cmd.MarkFlagRequired("entity-type")
	return cmd
}

func newTagListAllCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list-all",
		Short: opts.t("cli.tag.list_all.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ListAllTags(ctx)
			})
		},
	}
}

func newTagRemoveCommand(opts *runtimeOptions) *cobra.Command {
	var entityType string
	var entityID, tagID int64
	var confirmed bool
	cmd := &cobra.Command{
		Use:   "remove",
		Short: opts.t("cli.tag.remove.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().RemoveTag(ctx, operation.RemoveTagInput{
					ProjectSelector: operation.ProjectSelector{
						Project:   opts.project,
						ProjectID: opts.projectID,
					},
					EntityType: entityType,
					EntityID:   entityID,
					TagID:      tagID,
					Confirmed:  confirmed,
				})
			})
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", opts.t("cli.tag.flag.entity-type"))
	cmd.Flags().Int64Var(&entityID, "entity-id", 0, opts.t("cli.tag.flag.entity-id"))
	cmd.Flags().Int64Var(&tagID, "tag-id", 0, opts.t("cli.tag.flag.tag-id"))
	cmd.Flags().BoolVar(&confirmed, "confirm", false, opts.t("cli.tag.remove.flag.confirm"))
	_ = cmd.MarkFlagRequired("entity-type")
	_ = cmd.MarkFlagRequired("tag-id")
	return cmd
}

func newTagMergeCommand(opts *runtimeOptions) *cobra.Command {
	var sourceTagID, targetTagID int64
	cmd := &cobra.Command{
		Use:   "merge",
		Short: opts.t("cli.tag.merge.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().MergeTags(ctx, operation.MergeTagsInput{
					SourceTagID: sourceTagID,
					TargetTagID: targetTagID,
				})
			})
		},
	}
	cmd.Flags().Int64Var(&sourceTagID, "source", 0, opts.t("cli.tag.merge.flag.source"))
	cmd.Flags().Int64Var(&targetTagID, "target", 0, opts.t("cli.tag.merge.flag.target"))
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
	"omakiten/internal/domain"
	"omakiten/internal/knowledgefile"
)

type knowledgeCommandOptions struct {
	runtime        *runtimeOptions
	includeRelated bool
	kind           string
}

func newKnowledgeCommand(opts *runtimeOptions) *cobra.Command {
	options := &knowledgeCommandOptions{runtime: opts}
	cmd := &cobra.Command{Use: "knowledge", Short: opts.t("cli.knowledge.short")}
	cmd.PersistentFlags().BoolVar(&options.includeRelated, "include-related", false, opts.t("cli.knowledge.flag.include-related"))
	cmd.AddCommand(newKnowledgeValidateCommand(options))
	cmd.AddCommand(newKnowledgeListCommand(options))
	cmd.AddCommand(newKnowledgeShowCommand(options))
	cmd.AddCommand(newKnowledgeSearchCommand(options))
	return cmd
}

func (o *knowledgeCommandOptions) snapshot(ctx context.Context, includeRelated bool) (domain.KnowledgeSnapshot, domain.ProjectContext, error) {
	rt, err := o.runtime.open(ctx, false)
	if err != nil {
		return domain.KnowledgeSnapshot{}, domain.ProjectContext{}, err
	}
	defer rt.close()
	project, err := o.runtime.resolveProject(ctx, rt.store)
	if err != nil {
		return domain.KnowledgeSnapshot{}, domain.ProjectContext{}, err
	}
	snapshot := knowledgefile.Load(ctx, project, includeRelated || o.includeRelated, rt.store.FindProjectBySlug)
	if len(snapshot.Diagnostics) > 0 {
		return snapshot, project, domain.NewError(domain.ErrValidation, "knowledge sources contain errors", map[string]any{"diagnostics": snapshot.Diagnostics})
	}
	return snapshot, project, nil
}

func newKnowledgeValidateCommand(o *knowledgeCommandOptions) *cobra.Command {
	return &cobra.Command{Use: "validate", Short: o.runtime.t("cli.knowledge.validate.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(ctx context.Context) (any, error) {
			snapshot, _, err := o.snapshot(ctx, false)
			if err != nil {
				return nil, err
			}
			return map[string]any{"resources": len(snapshot.Resources), "relations": len(snapshot.Relations)}, nil
		})
	}}
}

func newKnowledgeListCommand(o *knowledgeCommandOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: o.runtime.t("cli.knowledge.list.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(ctx context.Context) (any, error) {
			snapshot, _, err := o.snapshot(ctx, false)
			if err != nil {
				return nil, err
			}
			return map[string]any{"resources": knowledgeMatches(snapshot, func(item domain.KnowledgeResource) bool {
				return o.kind == "" || strings.EqualFold(item.Kind, o.kind)
			})}, nil
		})
	}}
	cmd.Flags().StringVar(&o.kind, "type", "", o.runtime.t("cli.knowledge.flag.type"))
	return cmd
}

func newKnowledgeShowCommand(o *knowledgeCommandOptions) *cobra.Command {
	return &cobra.Command{Use: "show RESOURCE_ID", Short: o.runtime.t("cli.knowledge.show.short"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runJSON(cmd, func(ctx context.Context) (any, error) {
			id := args[0]
			external := !strings.HasPrefix(id, "markdown:") && !strings.HasPrefix(id, "openapi:") && !strings.HasPrefix(id, "cli:")
			snapshot, project, err := o.snapshot(ctx, external)
			if err != nil {
				return nil, err
			}
			return knowledgeResource(snapshot, project.Slug, id)
		})
	}}
}

func knowledgeResource(snapshot domain.KnowledgeSnapshot, project, id string) (any, error) {
	for _, item := range snapshot.Resources {
		if id != item.Project+":"+item.ID && (item.Project != project || id != item.ID) {
			continue
		}
		links := make([]domain.KnowledgeRelation, 0)
		for _, edge := range snapshot.Relations {
			if edge.From == item.Project+":"+item.ID {
				links = append(links, edge)
			}
		}
		return map[string]any{"resource": item, "relations": links}, nil
	}
	return nil, domain.NewError(domain.ErrValidation, "knowledge resource not found", map[string]any{"id": id})
}

func newKnowledgeSearchCommand(o *knowledgeCommandOptions) *cobra.Command {
	return &cobra.Command{Use: "search QUERY", Short: o.runtime.t("cli.knowledge.search.short"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runJSON(cmd, func(ctx context.Context) (any, error) {
			query := strings.ToLower(strings.TrimSpace(args[0]))
			if query == "" {
				return nil, domain.NewError(domain.ErrValidation, "search query is required", nil)
			}
			snapshot, _, err := o.snapshot(ctx, false)
			if err != nil {
				return nil, err
			}
			return map[string]any{"resources": knowledgeMatches(snapshot, func(item domain.KnowledgeResource) bool {
				return strings.Contains(strings.ToLower(item.Title+" "+item.Description+" "+item.Body), query)
			})}, nil
		})
	}}
}

func knowledgeMatches(snapshot domain.KnowledgeSnapshot, matches func(domain.KnowledgeResource) bool) []domain.KnowledgeResource {
	items := make([]domain.KnowledgeResource, 0)
	for _, item := range snapshot.Resources {
		if matches(item) {
			item.Body = ""
			items = append(items, item)
		}
	}
	return items
}

package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func newLawCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "law",
		Short: "Manage agent laws (file-backed under laws/)",
	}
	cmd.AddCommand(newLawListCommand(opts))
	cmd.AddCommand(newLawShowCommand(opts))
	cmd.AddCommand(newLawAddCommand(opts))
	cmd.AddCommand(newLawEditCommand(opts))
	cmd.AddCommand(newLawRemoveCommand(opts))
	return cmd
}

func newLawListCommand(opts *runtimeOptions) *cobra.Command {
	var scope, project, persona string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active laws (filterable by scope/project/persona)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.lawService()
				laws, err := service.ListFiltered(ctx, app.LawListFilter{Scope: scope, Project: project, Persona: persona})
				if err != nil {
					return nil, err
				}
				return map[string]any{"laws": laws}, nil
			})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope: global, project, or persona")
	cmd.Flags().StringVar(&project, "project", "", "filter by project slug")
	cmd.Flags().StringVar(&persona, "persona", "", "filter by persona slug")
	return cmd
}

func newLawShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show SLUG",
		Short: "Show a law (frontmatter + body)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				law, err := rt.lawService().Show(ctx, args[0])
				if err != nil {
					return nil, err
				}
				return map[string]any{"law": law}, nil
			})
		},
	}
}

func newLawAddCommand(opts *runtimeOptions) *cobra.Command {
	var key, name, severity, body, scope, project, persona string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a law (writes laws/<slug>.md and opens $EDITOR if body omitted)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.lawService()
				if body == "" {
					body = " "
				}
				severityID, err := parseSeverity(severity)
				if err != nil {
					return nil, err
				}
				law, err := service.Add(ctx, domain.LawInput{
					Key:      key,
					Name:     name,
					Severity: severityID,
					Body:     body,
					Scope:    domain.LawScope(scope),
					Project:  project,
					Persona:  persona,
				})
				if err != nil {
					return nil, err
				}
				if !noEdit {
					if err := openEditorAndReimport(ctx, rt, law.SourcePath); err != nil {
						return nil, err
					}
					law, err = service.Show(ctx, law.Key)
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"law": law}, nil
			})
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", "law slug")
	cmd.Flags().StringVarP(&name, "name", "n", "", "law display name (optional)")
	cmd.Flags().StringVarP(&severity, "severity", "s", "error", "info, warning, or error")
	cmd.Flags().StringVarP(&body, "body", "b", "", "law body (placeholder used if empty + $EDITOR opens)")
	cmd.Flags().StringVar(&scope, "scope", "global", "global, project, or persona")
	cmd.Flags().StringVar(&project, "project", "", "project slug (required when --scope=project)")
	cmd.Flags().StringVar(&persona, "persona", "", "persona slug (required when --scope=persona)")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "skip opening $EDITOR after creating the scaffold")
	_ = cmd.MarkFlagRequired("key")
	return cmd
}

func newLawEditCommand(opts *runtimeOptions) *cobra.Command {
	var name, severity, body string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "edit SLUG",
		Short: "Edit a law (opens $EDITOR by default; flags override frontmatter/body)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.lawService()
				slug, err := resolveLawSlug(ctx, service, args[0])
				if err != nil {
					return nil, err
				}
				if cmd.Flags().Changed("name") || cmd.Flags().Changed("severity") || cmd.Flags().Changed("body") {
					update := domain.LawUpdate{}
					if cmd.Flags().Changed("name") {
						update.Name = &name
					}
					if cmd.Flags().Changed("severity") {
						value, err := parseSeverity(severity)
						if err != nil {
							return nil, err
						}
						update.Severity = &value
					}
					if cmd.Flags().Changed("body") {
						update.Body = &body
					}
					if _, err := service.Edit(ctx, slug, update); err != nil {
						return nil, err
					}
				}
				law, err := service.Show(ctx, slug)
				if err != nil {
					return nil, err
				}
				if !noEdit {
					if err := openEditorAndReimport(ctx, rt, law.SourcePath); err != nil {
						return nil, err
					}
					law, err = service.Show(ctx, slug)
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"law": law}, nil
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "rewrite law display name")
	cmd.Flags().StringVarP(&severity, "severity", "s", "", "rewrite severity")
	cmd.Flags().StringVarP(&body, "body", "b", "", "rewrite law body")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "do not open $EDITOR (only apply flag-driven updates)")
	return cmd
}

func newLawRemoveCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove SLUG",
		Short: "Remove a law (deletes file + prunes refs)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.lawService()
				slug, err := resolveLawSlug(ctx, service, args[0])
				if err != nil {
					return nil, err
				}
				if err := service.Remove(ctx, slug); err != nil {
					return nil, err
				}
				return map[string]any{"removed": true, "slug": slug}, nil
			})
		},
	}
}

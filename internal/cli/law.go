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
		Short: opts.t("cli.law.short"),
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
		Short: opts.t("cli.law.list.short"),
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
	cmd.Flags().StringVar(&scope, "scope", "", opts.t("cli.law.list.flag.scope"))
	cmd.Flags().StringVar(&project, "project", "", opts.t("cli.law.list.flag.project"))
	cmd.Flags().StringVar(&persona, "persona", "", opts.t("cli.law.list.flag.persona"))
	return cmd
}

func newLawShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show SLUG",
		Short: opts.t("cli.law.show.short"),
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
		Short: opts.t("cli.law.add.short"),
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
				severityID, err := parseSeverity(severity, rt.activeRegistry())
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
	cmd.Flags().StringVarP(&key, "key", "k", "", opts.t("cli.law.add.flag.key"))
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.law.add.flag.name"))
	cmd.Flags().StringVarP(&severity, "severity", "s", "error", opts.t("cli.law.add.flag.severity"))
	cmd.Flags().StringVarP(&body, "body", "b", "", opts.t("cli.law.add.flag.body"))
	cmd.Flags().StringVar(&scope, "scope", "global", opts.t("cli.law.add.flag.scope"))
	cmd.Flags().StringVar(&project, "project", "", opts.t("cli.law.add.flag.project"))
	cmd.Flags().StringVar(&persona, "persona", "", opts.t("cli.law.add.flag.persona"))
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, opts.t("cli.law.add.flag.no-edit"))
	_ = cmd.MarkFlagRequired("key")
	return cmd
}

func newLawEditCommand(opts *runtimeOptions) *cobra.Command {
	var name, severity, body string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "edit SLUG",
		Short: opts.t("cli.law.edit.short"),
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
						value, err := parseSeverity(severity, rt.activeRegistry())
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
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.law.edit.flag.name"))
	cmd.Flags().StringVarP(&severity, "severity", "s", "", opts.t("cli.law.edit.flag.severity"))
	cmd.Flags().StringVarP(&body, "body", "b", "", opts.t("cli.law.edit.flag.body"))
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, opts.t("cli.law.edit.flag.no-edit"))
	return cmd
}

func newLawRemoveCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove SLUG",
		Short: opts.t("cli.law.remove.short"),
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

package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
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

				return rt.operationService().ListLaws(ctx, contract.ListLawsInput{
					Scope:   scope,
					Project: project,
					Persona: persona,
				})
			})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", opts.t("cli.law.list.flag.scope"))
	cmd.Flags().StringVar(&project, "scope-project", "", opts.t("cli.law.list.flag.project"))
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

				return rt.operationService().ShowLaw(ctx, contract.ShowLawInput{Slug: args[0]})
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
				return runLawAdd(ctx, rt, key, name, severity, body, scope, project, persona, noEdit)
			})
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", opts.t("cli.law.add.flag.key"))
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.law.add.flag.name"))
	cmd.Flags().StringVarP(&severity, "severity", "s", "error", opts.t("cli.law.add.flag.severity"))
	cmd.Flags().StringVarP(&body, "body", "b", "", opts.t("cli.law.add.flag.body"))
	cmd.Flags().StringVar(&scope, "scope", "global", opts.t("cli.law.add.flag.scope"))
	cmd.Flags().StringVar(&project, "scope-project", "", opts.t("cli.law.add.flag.project"))
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
				return runLawEdit(ctx, cmd, rt, args[0], name, severity, body, noEdit)
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.law.edit.flag.name"))
	cmd.Flags().StringVarP(&severity, "severity", "s", "", opts.t("cli.law.edit.flag.severity"))
	cmd.Flags().StringVarP(&body, "body", "b", "", opts.t("cli.law.edit.flag.body"))
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, opts.t("cli.law.edit.flag.no-edit"))
	return cmd
}

func runLawAdd(ctx context.Context, rt *runtime, key, name, severity, body, scope, project, persona string, noEdit bool) (any, error) {
	if body == "" {
		body = "# " + key + "\n"
	}
	severityID, err := parseSeverity(severity, rt.activeRegistry())
	if err != nil {
		return nil, err
	}
	service := rt.operationService()
	law, err := service.AddLaw(ctx, domain.LawInput{
		Key: key, Name: name, Severity: severityID, Body: body,
		Scope: domain.LawScope(scope), Project: project, Persona: persona,
	})
	if err != nil {
		return nil, err
	}
	if !noEdit {
		if err := openEditorAndReimport(ctx, rt, law.SourcePath); err != nil {
			return nil, err
		}
		law, err = service.LawEntity(ctx, law.Key)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"law": law}, nil
}

func runLawEdit(ctx context.Context, cmd *cobra.Command, rt *runtime, slug, name, severity, body string, noEdit bool) (any, error) {
	service := rt.operationService()
	update, changed, err := lawEditUpdate(cmd, rt, name, severity, body)
	if err != nil {
		return nil, err
	}
	if changed {
		if _, err := service.EditLaw(ctx, slug, update); err != nil {
			return nil, err
		}
	}
	law, err := service.LawEntity(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !noEdit {
		if err := openEditorAndReimport(ctx, rt, law.SourcePath); err != nil {
			return nil, err
		}
		law, err = service.LawEntity(ctx, slug)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"law": law}, nil
}

func lawEditUpdate(cmd *cobra.Command, rt *runtime, name, severity, body string) (domain.LawUpdate, bool, error) {
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("severity") && !cmd.Flags().Changed("body") {
		return domain.LawUpdate{}, false, nil
	}
	update := domain.LawUpdate{}
	if cmd.Flags().Changed("name") {
		update.Name = &name
	}
	if cmd.Flags().Changed("severity") {
		value, err := parseSeverity(severity, rt.activeRegistry())
		if err != nil {
			return domain.LawUpdate{}, false, err
		}
		update.Severity = &value
	}
	if cmd.Flags().Changed("body") {
		update.Body = &body
	}
	return update, true, nil
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

				service := rt.operationService()
				slug, err := service.RemoveLaw(ctx, args[0])
				if err != nil {
					return nil, err
				}
				return map[string]any{"removed": true, "slug": slug}, nil
			})
		},
	}
}

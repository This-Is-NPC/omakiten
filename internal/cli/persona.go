package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func newPersonaCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "persona",
		Short: opts.t("cli.persona.short"),
	}
	cmd.AddCommand(newPersonaListCommand(opts))
	cmd.AddCommand(newPersonaShowCommand(opts))
	cmd.AddCommand(newPersonaAddCommand(opts))
	cmd.AddCommand(newPersonaEditCommand(opts))
	cmd.AddCommand(newPersonaRemoveCommand(opts))
	return cmd
}

func newPersonaListCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: opts.t("cli.persona.list.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ListPersonas(ctx, contract.ListPersonasInput{})
			})
		},
	}
}

func newPersonaShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show SLUG",
		Short: opts.t("cli.persona.show.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				return rt.operationService().ShowPersona(ctx, contract.ShowPersonaInput{Slug: args[0]})
			})
		},
	}
}

func newPersonaAddCommand(opts *runtimeOptions) *cobra.Command {
	var key, name, description string
	var skillIDs []int64
	var skillSlugs []string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: opts.t("cli.persona.add.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return runPersonaAdd(ctx, rt, key, name, description, skillIDs, skillSlugs, noEdit)
			})
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", opts.t("cli.persona.add.flag.key"))
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.persona.add.flag.name"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.persona.add.flag.description"))
	cmd.Flags().Int64SliceVarP(&skillIDs, "skill", "s", nil, opts.t("cli.persona.add.flag.skill"))
	cmd.Flags().StringSliceVar(&skillSlugs, "skill-slug", nil, opts.t("cli.persona.add.flag.skill-slug"))
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, opts.t("cli.persona.add.flag.no-edit"))
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPersonaEditCommand(opts *runtimeOptions) *cobra.Command {
	var name, description string
	var skillIDs []int64
	var skillSlugs []string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "edit SLUG",
		Short: opts.t("cli.persona.edit.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return runPersonaEdit(ctx, cmd, rt, args[0], name, description, skillIDs, skillSlugs, noEdit)
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.persona.edit.flag.name"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.persona.edit.flag.description"))
	cmd.Flags().Int64SliceVarP(&skillIDs, "skill", "s", nil, opts.t("cli.persona.edit.flag.skill"))
	cmd.Flags().StringSliceVar(&skillSlugs, "skill-slug", nil, opts.t("cli.persona.edit.flag.skill-slug"))
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, opts.t("cli.persona.edit.flag.no-edit"))
	return cmd
}

func runPersonaAdd(ctx context.Context, rt *runtime, key, name, description string, skillIDs []int64, skillSlugs []string, noEdit bool) (any, error) {
	service := rt.operationService()
	persona, err := service.AddPersona(ctx, domain.PersonaInput{
		Key: key, Name: name, Description: description, SkillIDs: skillIDs, SkillKeys: skillSlugs,
	})
	if err != nil {
		return nil, err
	}
	if !noEdit {
		if err := openEditorAndReimport(ctx, rt, persona.SourcePath); err != nil {
			return nil, err
		}
		persona, err = service.PersonaEntity(ctx, persona.Key)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"persona": persona}, nil
}

func runPersonaEdit(ctx context.Context, cmd *cobra.Command, rt *runtime, slug, name, description string, skillIDs []int64, skillSlugs []string, noEdit bool) (any, error) {
	service := rt.operationService()
	if update, ok := personaEditUpdate(cmd, name, description, skillIDs, skillSlugs); ok {
		if _, err := service.EditPersona(ctx, slug, update); err != nil {
			return nil, err
		}
	}
	persona, err := service.PersonaEntity(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !noEdit {
		if err := openEditorAndReimport(ctx, rt, persona.SourcePath); err != nil {
			return nil, err
		}
		persona, err = service.PersonaEntity(ctx, slug)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"persona": persona}, nil
}

func personaEditUpdate(cmd *cobra.Command, name, description string, skillIDs []int64, skillSlugs []string) (domain.PersonaUpdate, bool) {
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("description") && !cmd.Flags().Changed("skill") && !cmd.Flags().Changed("skill-slug") {
		return domain.PersonaUpdate{}, false
	}
	update := domain.PersonaUpdate{}
	if cmd.Flags().Changed("name") {
		update.Name = &name
	}
	if cmd.Flags().Changed("description") {
		update.Description = &description
	}
	if cmd.Flags().Changed("skill") {
		ids := append([]int64(nil), skillIDs...)
		update.SkillIDs = &ids
	}
	if cmd.Flags().Changed("skill-slug") {
		slugs := append([]string(nil), skillSlugs...)
		update.SkillKeys = &slugs
	}
	return update, true
}

func newPersonaRemoveCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove SLUG",
		Short: opts.t("cli.persona.remove.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.operationService()
				slug, err := service.RemovePersona(ctx, args[0])
				if err != nil {
					return nil, err
				}
				return map[string]any{"removed": true, "slug": slug}, nil
			})
		},
	}
}

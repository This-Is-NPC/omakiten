package cli

import (
	"context"

	"github.com/spf13/cobra"

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

				personas, err := rt.personaService().List(ctx)
				if err != nil {
					return nil, err
				}
				return map[string]any{"personas": personas}, nil
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

				persona, err := rt.personaService().Show(ctx, args[0])
				if err != nil {
					return nil, err
				}
				return map[string]any{"persona": persona}, nil
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

				service := rt.personaService()
				persona, err := service.Add(ctx, domain.PersonaInput{
					Key:         key,
					Name:        name,
					Description: description,
					SkillIDs:    skillIDs,
					SkillKeys:   skillSlugs,
				})
				if err != nil {
					return nil, err
				}
				if !noEdit {
					if err := openEditorAndReimport(ctx, rt, persona.SourcePath); err != nil {
						return nil, err
					}
					persona, err = service.Show(ctx, persona.Key)
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"persona": persona}, nil
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

				service := rt.personaService()
				slug, err := resolvePersonaSlug(ctx, service, args[0])
				if err != nil {
					return nil, err
				}
				if cmd.Flags().Changed("name") || cmd.Flags().Changed("description") || cmd.Flags().Changed("skill") || cmd.Flags().Changed("skill-slug") {
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
					if _, err := service.Edit(ctx, slug, update); err != nil {
						return nil, err
					}
				}
				persona, err := service.Show(ctx, slug)
				if err != nil {
					return nil, err
				}
				if !noEdit {
					if err := openEditorAndReimport(ctx, rt, persona.SourcePath); err != nil {
						return nil, err
					}
					persona, err = service.Show(ctx, slug)
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"persona": persona}, nil
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

				service := rt.personaService()
				slug, err := resolvePersonaSlug(ctx, service, args[0])
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

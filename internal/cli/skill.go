package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
)

func newSkillCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: opts.t("cli.skill.short"),
	}

	cmd.AddCommand(newSkillListCommand(opts))
	cmd.AddCommand(newSkillShowCommand(opts))
	cmd.AddCommand(newSkillAddCommand(opts))
	cmd.AddCommand(newSkillEditCommand(opts))
	cmd.AddCommand(newSkillRemoveCommand(opts))
	return cmd
}

func newSkillListCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: opts.t("cli.skill.list.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.skillService()
				skills, err := service.List(ctx)
				if err != nil {
					return nil, err
				}
				return map[string]any{"skills": skills}, nil
			})
		},
	}
}

func newSkillShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show SLUG",
		Short: opts.t("cli.skill.show.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				skill, err := rt.skillService().Show(ctx, args[0])
				if err != nil {
					return nil, err
				}
				return map[string]any{"skill": skill}, nil
			})
		},
	}
}

func newSkillAddCommand(opts *runtimeOptions) *cobra.Command {
	var key, name, description string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: opts.t("cli.skill.add.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.skillService()
				skill, err := service.Add(ctx, domain.SkillInput{Key: key, Name: name, Description: description})
				if err != nil {
					return nil, err
				}
				if !noEdit {
					if err := openEditorAndReimport(ctx, rt, skill.SourcePath); err != nil {
						return nil, err
					}
					skill, err = service.Show(ctx, skill.Key)
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"skill": skill}, nil
			})
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", opts.t("cli.skill.add.flag.key"))
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.skill.add.flag.name"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.skill.add.flag.description"))
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, opts.t("cli.skill.add.flag.no-edit"))
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newSkillEditCommand(opts *runtimeOptions) *cobra.Command {
	var name, description string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "edit SLUG",
		Short: opts.t("cli.skill.edit.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.skillService()
				slug, err := resolveSkillSlug(ctx, service, args[0])
				if err != nil {
					return nil, err
				}
				if cmd.Flags().Changed("name") || cmd.Flags().Changed("description") {
					update := domain.SkillUpdate{}
					if cmd.Flags().Changed("name") {
						update.Name = &name
					}
					if cmd.Flags().Changed("description") {
						update.Description = &description
					}
					if _, err := service.Edit(ctx, slug, update); err != nil {
						return nil, err
					}
				}
				skill, err := service.Show(ctx, slug)
				if err != nil {
					return nil, err
				}
				if !noEdit {
					if err := openEditorAndReimport(ctx, rt, skill.SourcePath); err != nil {
						return nil, err
					}
					skill, err = service.Show(ctx, slug)
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"skill": skill}, nil
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.skill.edit.flag.name"))
	cmd.Flags().StringVarP(&description, "description", "d", "", opts.t("cli.skill.edit.flag.description"))
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, opts.t("cli.skill.edit.flag.no-edit"))
	return cmd
}

func newSkillRemoveCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove SLUG",
		Short: opts.t("cli.skill.remove.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()

				service := rt.skillService()
				slug, err := resolveSkillSlug(ctx, service, args[0])
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

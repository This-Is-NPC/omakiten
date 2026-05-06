package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
)

func newSkillCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage skills (technical tags, file-backed under skills/)",
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
		Short: "List active skills",
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
		Short: "Show a skill (frontmatter + body)",
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
		Short: "Add a skill (writes skills/<slug>.md and opens $EDITOR)",
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
	cmd.Flags().StringVarP(&key, "key", "k", "", "skill slug (defaults to slugify(--name))")
	cmd.Flags().StringVarP(&name, "name", "n", "", "skill display name")
	cmd.Flags().StringVarP(&description, "description", "d", "", "short description")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "skip opening $EDITOR after creating the scaffold")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newSkillEditCommand(opts *runtimeOptions) *cobra.Command {
	var name, description string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "edit SLUG",
		Short: "Edit a skill (opens $EDITOR by default; flags override frontmatter)",
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
	cmd.Flags().StringVarP(&name, "name", "n", "", "rewrite skill display name")
	cmd.Flags().StringVarP(&description, "description", "d", "", "rewrite skill description")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "do not open $EDITOR (only apply flag-driven updates)")
	return cmd
}

func newSkillRemoveCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove SLUG",
		Short: "Remove a skill (deletes file + prunes refs)",
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

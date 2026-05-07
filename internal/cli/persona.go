package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/domain"
)

func newPersonaCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "persona",
		Short: "Manage personas (file-backed under personas/)",
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
		Short: "List active personas",
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
		Short: "Show a persona (frontmatter + body + skill refs)",
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
		Short: "Add a persona (writes personas/<slug>.md and opens $EDITOR)",
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
	cmd.Flags().StringVarP(&key, "key", "k", "", "persona slug (defaults to slugify(--name))")
	cmd.Flags().StringVarP(&name, "name", "n", "", "persona display name")
	cmd.Flags().StringVarP(&description, "description", "d", "", "short description")
	cmd.Flags().Int64SliceVarP(&skillIDs, "skill", "s", nil, "skill ids (repeatable, e.g. -s 1 -s 2)")
	cmd.Flags().StringSliceVar(&skillSlugs, "skill-slug", nil, "skill slugs (repeatable)")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "skip opening $EDITOR after creating the scaffold")
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
		Short: "Edit a persona (opens $EDITOR by default; flags override)",
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
	cmd.Flags().StringVarP(&name, "name", "n", "", "rewrite persona display name")
	cmd.Flags().StringVarP(&description, "description", "d", "", "rewrite persona description")
	cmd.Flags().Int64SliceVarP(&skillIDs, "skill", "s", nil, "skill ids (replaces existing set)")
	cmd.Flags().StringSliceVar(&skillSlugs, "skill-slug", nil, "skill slugs (replaces existing set)")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "do not open $EDITOR (only apply flag-driven updates)")
	return cmd
}

func newPersonaRemoveCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove SLUG",
		Short: "Remove a persona (deletes file + prunes refs)",
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

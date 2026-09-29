package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// newPlanCommand assembles plan, wave and atomic task-claim commands.
func newPlanCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: opts.t("cli.plan.short"),
	}
	cmd.AddCommand(newPlanCreateCommand(opts))
	cmd.AddCommand(newWorkImportCommand(opts, "plan"))
	cmd.AddCommand(newWorkExportCommand(opts, "plan"))
	cmd.AddCommand(newPlanListCommand(opts))
	cmd.AddCommand(newPlanShowCommand(opts))
	cmd.AddCommand(newPlanContinueCommand(opts))
	cmd.AddCommand(newPlanWaveAddCommand(opts))
	cmd.AddCommand(newPlanAssignCommand(opts))
	cmd.AddCommand(newPlanClaimCommand(opts))
	cmd.AddCommand(newPlanEditCommand(opts))
	cmd.AddCommand(newPlanDeleteCommand(opts))
	cmd.AddCommand(newPlanWaveRemoveCommand(opts))
	cmd.AddCommand(newPlanWaveRenameCommand(opts))
	cmd.AddCommand(newPlanWaveReorderCommand(opts))
	cmd.AddCommand(newPlanUnassignCommand(opts))
	return cmd
}

// newPlanWaveRemoveCommand wires `okt plan wave-remove WAVE_ID --confirm`.
// The destructive op requires --confirm; the wave's tasks survive with
// wave_id cleared (plan_id intact).
func newPlanWaveRemoveCommand(opts *runtimeOptions) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "wave-remove WAVE_ID",
		Short: opts.t("cli.plan.wave_remove.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				waveID, err := parseID(args[0], "wave id")
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().RemovePlanWave(ctx, contract.RemovePlanWaveInput{
					ProjectSelector: opts.projectSelector(),
					WaveID:          waveID,
					Confirmed:       confirm,
				})
			})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.plan.wave_remove.flag.confirm"))
	return cmd
}

// newPlanWaveRenameCommand wires `okt plan wave-rename WAVE_ID NAME`.
func newPlanWaveRenameCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "wave-rename WAVE_ID NAME",
		Short: opts.t("cli.plan.wave_rename.short"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				waveID, err := parseID(args[0], "wave id")
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().RenamePlanWave(ctx, contract.RenamePlanWaveInput{
					ProjectSelector: opts.projectSelector(),
					WaveID:          waveID,
					Name:            args[1],
				})
			})
		},
	}
}

// newPlanWaveReorderCommand wires `okt plan wave-reorder WAVE_ID POSITION`.
// The position is 1-based; colliding with an occupied slot swaps the two
// waves.
func newPlanWaveReorderCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "wave-reorder WAVE_ID POSITION",
		Short: opts.t("cli.plan.wave_reorder.short"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				waveID, err := parseID(args[0], "wave id")
				if err != nil {
					return nil, err
				}
				position, err := strconv.Atoi(args[1])
				if err != nil || position <= 0 {
					return nil, domain.NewError(domain.ErrValidation, fmt.Sprintf(t("cli.err.id_positive_fmt"), "position"), map[string]any{"value": args[1]})
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ReorderPlanWave(ctx, contract.ReorderPlanWaveInput{
					ProjectSelector: opts.projectSelector(),
					WaveID:          waveID,
					Position:        position,
				})
			})
		},
	}
}

// newPlanUnassignCommand wires `okt plan unassign TASK_ID`. Detaches the
// task from its plan, clearing both plan_id and wave_id.
func newPlanUnassignCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "unassign TASK_ID",
		Short: opts.t("cli.plan.unassign.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				taskID, err := parseID(args[0], "task id")
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().UnassignPlanTask(ctx, contract.UnassignPlanTaskInput{
					ProjectSelector: opts.projectSelector(),
					TaskID:          taskID,
				})
			})
		},
	}
}

// newPlanEditCommand wires `okt plan edit SLUG [--name --slug --status
// --goal-body]`. Only flags the user explicitly set are forwarded —
// goal_body edits route through UpdateGoalBody (plan.goal_edited) while
// name/slug/status route through UpdatePlan (plan.edited, plus
// plan.abandoned on an abandon). At least one flag is required.
func newPlanEditCommand(opts *runtimeOptions) *cobra.Command {
	var name, slug, status, goalBody string
	cmd := &cobra.Command{
		Use:   "edit SLUG",
		Short: opts.t("cli.plan.edit.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				input, err := editPlanInput(cmd, opts, args[0], name, slug, status, goalBody)
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().EditPlan(ctx, input)
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.plan.edit.flag.name"))
	cmd.Flags().StringVarP(&slug, "slug", "s", "", opts.t("cli.plan.edit.flag.slug"))
	cmd.Flags().StringVar(&status, "status", "", opts.t("cli.plan.edit.flag.status"))
	cmd.Flags().StringVarP(&goalBody, "goal-body", "g", "", opts.t("cli.plan.edit.flag.goal_body"))
	return cmd
}

func editPlanInput(cmd *cobra.Command, opts *runtimeOptions, planSlug, name, slug, status, goalBody string) (contract.EditPlanInput, error) {
	input := contract.EditPlanInput{ProjectSelector: opts.projectSelector(), Slug: planSlug}
	if cmd.Flags().Changed("name") {
		input.Name = &name
	}
	if cmd.Flags().Changed("slug") {
		input.NewSlug = &slug
	}
	if cmd.Flags().Changed("status") {
		input.Status = &status
	}
	if cmd.Flags().Changed("goal-body") {
		input.GoalBody = &goalBody
	}
	if input.Name == nil && input.NewSlug == nil && input.Status == nil && input.GoalBody == nil {
		return contract.EditPlanInput{}, domain.NewError(domain.ErrValidation,
			"plan edit requires at least one of --name, --slug, --status, --goal-body", nil)
	}
	return input, nil
}

// newPlanDeleteCommand wires `okt plan delete SLUG --confirm`. The
// destructive op requires --confirm; waves cascade-delete and member
// tasks survive detached (plan_id / wave_id cleared).
func newPlanDeleteCommand(opts *runtimeOptions) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete SLUG",
		Short: opts.t("cli.plan.delete.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().DeletePlan(ctx, contract.DeletePlanInput{
					ProjectSelector: opts.projectSelector(),
					Slug:            args[0],
					Confirmed:       confirm,
				})
			})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.plan.delete.flag.confirm"))
	return cmd
}

func newPlanCreateCommand(opts *runtimeOptions) *cobra.Command {
	var name string
	var goalBody, file string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "create [SLUG]",
		Short: opts.t("cli.plan.create.short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				if file != "" {
					if len(args) != 0 {
						return nil, domain.NewError(domain.ErrValidation, "--file supplies the plan slug", nil)
					}
					return importWorkFile(ctx, cmd, opts, "plan", file, dryRun, true)
				}
				if len(args) != 1 || name == "" || dryRun {
					return nil, domain.NewError(domain.ErrValidation, "provide --file or a slug and --name; --dry-run requires --file", nil)
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().CreatePlan(ctx, contract.CreatePlanInput{
					ProjectSelector: opts.projectSelector(),
					Slug:            args[0],
					Name:            name,
					GoalBody:        goalBody,
				})
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.plan.create.flag.name"))
	cmd.Flags().StringVarP(&goalBody, "goal-body", "g", "", opts.t("cli.plan.create.flag.goal_body"))
	cmd.Flags().StringVar(&file, "file", "", opts.t("cli.work.flag.file"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, opts.t("cli.work.flag.dry_run"))
	cmd.MarkFlagsMutuallyExclusive("file", "goal-body")
	cmd.MarkFlagsMutuallyExclusive("file", "name")
	return cmd
}

func newPlanListCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: opts.t("cli.plan.list.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ListPlans(ctx, contract.ListPlansInput{
					ProjectSelector: opts.projectSelector(),
				})
			})
		},
	}
}

func newPlanContinueCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "continue SLUG",
		Short: opts.t("cli.plan.continue.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ContinuePlan(ctx, contract.ContinuePlanInput{
					ProjectSelector: opts.projectSelector(),
					Slug:            args[0],
				})
			})
		},
	}
}

func newPlanShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show SLUG",
		Short: opts.t("cli.plan.show.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ShowPlan(ctx, contract.ShowPlanInput{
					ProjectSelector: opts.projectSelector(),
					Slug:            args[0],
				})
			})
		},
	}
}

func newPlanWaveAddCommand(opts *runtimeOptions) *cobra.Command {
	var position int
	cmd := &cobra.Command{
		Use:   "wave-add SLUG NAME",
		Short: opts.t("cli.plan.wave_add.short"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().AddPlanWave(ctx, contract.AddPlanWaveInput{
					ProjectSelector: opts.projectSelector(),
					Slug:            args[0],
					Name:            args[1],
					Position:        position,
				})
			})
		},
	}
	cmd.Flags().IntVar(&position, "position", 0, opts.t("cli.plan.wave_add.flag.position"))
	return cmd
}

func newPlanAssignCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign SLUG WAVE_ID TASK_ID",
		Short: opts.t("cli.plan.assign.short"),
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				waveID, err := parseID(args[1], "wave id")
				if err != nil {
					return nil, err
				}
				taskID, err := parseID(args[2], "task id")
				if err != nil {
					return nil, err
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().AssignPlanTask(ctx, contract.AssignPlanTaskInput{
					ProjectSelector: opts.projectSelector(),
					Slug:            args[0],
					WaveID:          waveID,
					TaskID:          taskID,
				})
			})
		},
	}
	return cmd
}

func newPlanClaimCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claim SLUG",
		Short: opts.t("cli.plan.claim.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().ClaimNextPlanTask(ctx, contract.ClaimNextPlanTaskInput{
					ProjectSelector: opts.projectSelector(),
					Slug:            args[0],
				})
			})
		},
	}
	return cmd
}

package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// newPlanCommand assembles the `okt plan ...` subcommand tree. Plans
// group child tasks into ordered waves and feed the multi-agent claim
// flow exposed at MCP (plans.claim_next).
func newPlanCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: opts.t("cli.plan.short"),
	}
	cmd.AddCommand(newPlanCreateCommand(opts))
	cmd.AddCommand(newPlanListCommand(opts))
	cmd.AddCommand(newPlanShowCommand(opts))
	cmd.AddCommand(newPlanWaveAddCommand(opts))
	cmd.AddCommand(newPlanAssignCommand(opts))
	cmd.AddCommand(newPlanClaimCommand(opts))
	cmd.AddCommand(newPlanEditCommand(opts))
	cmd.AddCommand(newPlanDeleteCommand(opts))
	return cmd
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
				var namePtr, slugPtr, statusPtr *string
				if cmd.Flags().Changed("name") {
					namePtr = &name
				}
				if cmd.Flags().Changed("slug") {
					slugPtr = &slug
				}
				if cmd.Flags().Changed("status") {
					statusPtr = &status
				}
				goalChanged := cmd.Flags().Changed("goal-body")
				if namePtr == nil && slugPtr == nil && statusPtr == nil && !goalChanged {
					return nil, domain.NewError(domain.ErrValidation,
						"plan edit requires at least one of --name, --slug, --status, --goal-body", nil)
				}

				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				svc := app.NewPlanService(rt.store)
				plan, err := svc.GetBySlug(ctx, project, args[0])
				if err != nil {
					return nil, err
				}
				if goalChanged {
					plan, err = svc.UpdateGoalBody(ctx, project, plan.ID, goalBody)
					if err != nil {
						return nil, err
					}
				}
				if namePtr != nil || slugPtr != nil || statusPtr != nil {
					plan, err = svc.UpdatePlan(ctx, project, plan.ID, namePtr, slugPtr, statusPtr)
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"project": project, "plan": plan}, nil
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.plan.edit.flag.name"))
	cmd.Flags().StringVarP(&slug, "slug", "s", "", opts.t("cli.plan.edit.flag.slug"))
	cmd.Flags().StringVar(&status, "status", "", opts.t("cli.plan.edit.flag.status"))
	cmd.Flags().StringVarP(&goalBody, "goal-body", "g", "", opts.t("cli.plan.edit.flag.goal_body"))
	return cmd
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
				if !confirm {
					return nil, domain.NewError(domain.ErrValidation,
						"plan delete is destructive (waves cascade, tasks detach); pass --confirm to proceed", nil)
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				svc := app.NewPlanService(rt.store)
				plan, err := svc.GetBySlug(ctx, project, args[0])
				if err != nil {
					return nil, err
				}
				if _, err := svc.DeletePlan(ctx, project, plan.ID); err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "deleted": plan.Slug}, nil
			})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, opts.t("cli.plan.delete.flag.confirm"))
	return cmd
}

func newPlanCreateCommand(opts *runtimeOptions) *cobra.Command {
	var name string
	var goalBody string
	cmd := &cobra.Command{
		Use:   "create SLUG --name NAME",
		Short: opts.t("cli.plan.create.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				plan, err := app.NewPlanService(rt.store).Create(ctx, project, args[0], name, goalBody)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "plan": plan}, nil
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", opts.t("cli.plan.create.flag.name"))
	cmd.Flags().StringVarP(&goalBody, "goal-body", "g", "", opts.t("cli.plan.create.flag.goal_body"))
	_ = cmd.MarkFlagRequired("name")
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
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				plans, err := app.NewPlanService(rt.store).List(ctx, project)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "plans": plans}, nil
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
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				show, err := app.NewPlanServiceWithSnapshot(rt.store, rt.activeSnapshot()).Show(ctx, project, args[0])
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "plan": show}, nil
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
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				svc := app.NewPlanService(rt.store)
				plan, err := svc.GetBySlug(ctx, project, args[0])
				if err != nil {
					return nil, err
				}
				wave, err := svc.AddWave(ctx, project, plan.ID, args[1], position)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "wave": wave}, nil
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
				waveID, err := strconv.ParseInt(args[1], 10, 64)
				if err != nil {
					return nil, domain.NewError(domain.ErrValidation, "wave id is not numeric", map[string]any{"value": args[1]})
				}
				taskID, err := strconv.ParseInt(args[2], 10, 64)
				if err != nil {
					return nil, domain.NewError(domain.ErrValidation, "task id is not numeric", map[string]any{"value": args[2]})
				}
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				svc := app.NewPlanService(rt.store)
				plan, err := svc.GetBySlug(ctx, project, args[0])
				if err != nil {
					return nil, err
				}
				if err := svc.AssignTask(ctx, project, taskID, plan.ID, waveID); err != nil {
					return nil, err
				}
				return map[string]any{"project": project, "task_id": taskID, "plan_id": plan.ID, "wave_id": waveID}, nil
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
				ctx = rt.WithActivityRepo(ctx)

				project, err := opts.resolveProject(ctx, rt.store)
				if err != nil {
					return nil, err
				}
				svc := app.NewPlanServiceWithSnapshot(rt.store, rt.activeSnapshot())
				plan, err := svc.GetBySlug(ctx, project, args[0])
				if err != nil {
					return nil, err
				}
				task, claimed, err := svc.ClaimNext(ctx, project, plan.ID)
				if err != nil {
					return nil, err
				}
				resp := map[string]any{"project": project, "claimed": claimed}
				if claimed {
					resp["task"] = task
				}
				return resp, nil
			})
		},
	}
	return cmd
}

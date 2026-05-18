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
// flow exposed at MCP (plans.claim_next). All CLI strings are inline
// English in this slice; the i18n key catalog migration ships in a
// follow-up so the language-pack parity test stays green without
// touching the 21 bundled packs in this slice.
func newPlanCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Manage WBS-style implementation plans and their waves",
	}
	cmd.AddCommand(newPlanCreateCommand(opts))
	cmd.AddCommand(newPlanListCommand(opts))
	cmd.AddCommand(newPlanShowCommand(opts))
	cmd.AddCommand(newPlanWaveAddCommand(opts))
	cmd.AddCommand(newPlanAssignCommand(opts))
	cmd.AddCommand(newPlanClaimCommand(opts))
	return cmd
}

func newPlanCreateCommand(opts *runtimeOptions) *cobra.Command {
	var name string
	var goalBody string
	cmd := &cobra.Command{
		Use:   "create SLUG --name NAME",
		Short: "Create a plan in the active project",
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
	cmd.Flags().StringVarP(&name, "name", "n", "", "Plan name (human-readable)")
	cmd.Flags().StringVarP(&goalBody, "goal-body", "g", "", "Optional markdown goal / acceptance body")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPlanListCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List plans in the active project",
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
		Short: "Show a plan with its waves, tasks per wave, and done/total counts",
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
		Short: "Append a wave to a plan (auto-position) or insert at --position",
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
	cmd.Flags().IntVar(&position, "position", 0, "Wave position (1-based); 0 appends after the current highest")
	return cmd
}

func newPlanAssignCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign SLUG WAVE_ID TASK_ID",
		Short: "Attach an existing task to a (plan, wave)",
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
		Short: "Atomically claim the next unblocked task in the plan's active wave (requires OMAKITEN_AGENT_MODEL)",
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

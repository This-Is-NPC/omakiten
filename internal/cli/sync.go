package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newRemoteCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: opts.t("cli.remote.short"),
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set <url>",
		Short: opts.t("cli.remote.set.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runSyncOperation(ctx, opts, func(ctx context.Context, rt *runtime) (any, error) {
					return rt.operationService().SetSyncRemote(ctx, contract.SetSyncRemoteInput{URL: args[0]})
				})
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: opts.t("cli.remote.show.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runSyncOperation(ctx, opts, func(ctx context.Context, rt *runtime) (any, error) {
					return rt.operationService().ShowSyncRemote(ctx)
				})
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "projects",
		Short: opts.t("cli.remote.projects.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runSyncOperation(ctx, opts, func(ctx context.Context, rt *runtime) (any, error) {
					return rt.operationService().ListSyncRemoteProjects(ctx)
				})
			})
		},
	})
	return cmd
}

func newSyncCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: opts.t("cli.sync.short"),
		Long:  opts.t("cli.sync.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runSyncOperation(ctx, opts, func(ctx context.Context, rt *runtime) (any, error) {
					return rt.operationService().Sync(ctx, contract.SyncInput{ProjectSelector: opts.projectSelector()})
				})
			})
		},
	}
}

func newProjectsCloneCommand(opts *runtimeOptions) *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "clone <uid>",
		Short: opts.t("cli.projects.clone.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				return runSyncOperation(ctx, opts, func(ctx context.Context, rt *runtime) (any, error) {
					return rt.operationService().CloneProject(ctx, contract.CloneProjectInput{UID: args[0], RootPath: root})
				})
			})
		},
	}
	cmd.Flags().StringVar(&root, "root", "", opts.t("cli.projects.clone.flag.root"))
	_ = cmd.MarkFlagRequired("root")
	return cmd
}

// runSyncOperation opens the runtime for one sync operation and closes it afterwards.
func runSyncOperation(ctx context.Context, opts *runtimeOptions, run func(context.Context, *runtime) (any, error)) (any, error) {
	rt, err := opts.open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer rt.close()
	return run(ctx, rt)
}

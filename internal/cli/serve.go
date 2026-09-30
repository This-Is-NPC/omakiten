package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"omakiten/internal/agentruntime"
	"omakiten/internal/contract"
)

func newServeCommand(opts *runtimeOptions, version string, run func(context.Context, agentruntime.Session, contract.ServeOptions) error) *cobra.Command {
	serve := contract.ServeOptions{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: opts.t("cli.serve.short"),
		Long:  opts.t("cli.serve.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if run == nil {
				return fmt.Errorf("server runner is not installed")
			}
			session, rt, err := opts.openSession(cmd.Context(), version)
			if err != nil {
				return err
			}
			defer rt.close()
			serve.Stderr = cmd.ErrOrStderr()
			return run(cmd.Context(), session, serve)
		},
	}
	cmd.Flags().StringVar(&serve.Addr, "addr", "127.0.0.1:0", opts.t("cli.serve.flag.addr"))
	cmd.Flags().DurationVar(&serve.Poll, "poll", 250*time.Millisecond, opts.t("cli.serve.flag.poll"))
	return cmd
}

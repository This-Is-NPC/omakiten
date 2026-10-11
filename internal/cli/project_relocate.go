package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/contract"
)

func newProjectRelocateCommand(opts *runtimeOptions) *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "relocate",
		Short: opts.t("cli.project.relocate.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				// Recovery uses the application configuration rather than the missing old root.
				options := *opts
				options.project = ""
				options.projectID = 0
				path, err := resolveActiveFileForScope(opts, "global")
				if err != nil {
					return nil, err
				}
				options.configPath = path
				rt, err := options.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().RelocateProject(ctx, contract.RelocateProjectInput{ProjectSelector: opts.projectSelector(), RootPath: root})
			})
		},
	}
	cmd.Flags().StringVar(&root, "root", "", opts.t("cli.project.relocate.flag.root"))
	_ = cmd.MarkFlagRequired("root")
	return cmd
}

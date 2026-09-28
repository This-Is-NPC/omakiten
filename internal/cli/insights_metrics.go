package cli

import (
	"context"

	"github.com/spf13/cobra"

	"omakiten/internal/operation"
)

func newInsightsCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: opts.t("cli.insights.short"),
	}
	cmd.AddCommand(newInsightsSummaryCommand(opts))
	return cmd
}

func newInsightsSummaryCommand(opts *runtimeOptions) *cobra.Command {
	var stuckDays int
	cmd := &cobra.Command{
		Use:   "summary",
		Short: opts.t("cli.insights.summary.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				return rt.operationService().InsightsSummary(ctx, operation.InsightsSummaryInput{
					ProjectSelector: opts.projectSelector(),
					StuckDays:       stuckDays,
				})
			})
		},
	}
	cmd.Flags().IntVar(&stuckDays, "stuck-days", 0, opts.t("cli.insights.summary.flag.stuck-days"))
	return cmd
}

func newMetricsCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: opts.t("cli.metrics.short"),
	}
	cmd.AddCommand(newMetricsSummaryCommand(opts))
	return cmd
}

func newMetricsSummaryCommand(opts *runtimeOptions) *cobra.Command {
	var period string
	cmd := &cobra.Command{
		Use:   "summary",
		Short: opts.t("cli.metrics.summary.short"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				rt, err := opts.open(ctx, true)
				if err != nil {
					return nil, err
				}
				defer rt.close()
				sel := opts.projectSelector()
				return rt.operationService().MetricsSummary(ctx, operation.MetricsSummaryInput{
					ProjectSelector: sel,
					Period:          period,
					ProjectID:       sel.ProjectID,
				})
			})
		},
	}
	cmd.Flags().StringVar(&period, "period", "", opts.t("cli.metrics.summary.flag.period"))
	return cmd
}

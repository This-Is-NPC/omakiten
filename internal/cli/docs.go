package cli

import (
	"context"

	"github.com/spf13/cobra"
	"omakiten/docs"
	"omakiten/internal/domain"
	"omakiten/internal/knowledgefile"
)

func newDocsCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "docs", Short: opts.t("cli.docs.short")}
	cmd.AddCommand(newDocsListCommand(opts), newDocsShowCommand(opts))
	return cmd
}

func newDocsListCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{Use: "list", Short: opts.t("cli.docs.list.short"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(context.Context) (any, error) {
			guides, err := knowledgefile.ReadGuides(docs.FS)
			if err != nil {
				return nil, err
			}
			for i := range guides {
				guides[i].Markdown = ""
			}
			return map[string]any{"guides": guides}, nil
		})
	}}
}

func newDocsShowCommand(opts *runtimeOptions) *cobra.Command {
	return &cobra.Command{Use: "show TOPIC", Short: opts.t("cli.docs.show.short"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runJSON(cmd, func(context.Context) (any, error) {
			return shippedGuide(args[0])
		})
	}}
}

// shippedGuide returns the guide named topic, or a validation error that
// lists every topic.
func shippedGuide(topic string) (knowledgefile.Guide, error) {
	guides, err := knowledgefile.ReadGuides(docs.FS)
	if err != nil {
		return knowledgefile.Guide{}, err
	}
	topics := make([]string, len(guides))
	for i, guide := range guides {
		if guide.Topic == topic {
			return guide, nil
		}
		topics[i] = guide.Topic
	}
	return knowledgefile.Guide{}, domain.NewError(domain.ErrValidation, "unknown docs topic", map[string]any{"topic": topic, "topics": topics})
}

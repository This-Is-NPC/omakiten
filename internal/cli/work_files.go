package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
	"omakiten/internal/workfile"
)

func readWorkFile(cmd *cobra.Command, path string) ([]byte, error) {
	reader := cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	data, err := workfile.Read(reader)
	if err != nil {
		return nil, domain.NewError(domain.ErrValidation, err.Error(), nil)
	}
	return data, nil
}

func importWorkFile(ctx context.Context, cmd *cobra.Command, opts *runtimeOptions, kind, path string, dryRun, confirmed bool) (any, error) {
	data, err := readWorkFile(cmd, path)
	if err != nil {
		return nil, err
	}
	doc, err := workfile.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, domain.NewError(domain.ErrValidation, err.Error(), nil)
	}
	return importWorkDocument(ctx, opts, kind, doc, dryRun, confirmed)
}

func importWorkDocument(ctx context.Context, opts *runtimeOptions, kind string, doc domain.WorkDocument, dryRun, confirmed bool) (any, error) {
	rt, err := opts.open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer rt.close()
	input := contract.ImportWorkInput{ProjectSelector: opts.projectSelector(), Document: doc, DryRun: dryRun, Confirmed: confirmed}
	if kind == "plan" {
		return rt.operationService().ImportPlan(ctx, input)
	}
	return rt.operationService().ImportTask(ctx, input)
}

func newWorkImportCommand(opts *runtimeOptions, kind string) *cobra.Command {
	var file string
	var dryRun, confirmed bool
	cmd := &cobra.Command{Use: "import --file PATH", Short: opts.t("cli.work.import.short"), Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runJSON(cmd, func(ctx context.Context) (any, error) {
			return importWorkFile(ctx, cmd, opts, kind, file, dryRun, confirmed)
		})
	}
	cmd.Flags().StringVar(&file, "file", "", opts.t("cli.work.flag.file"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, opts.t("cli.work.flag.dry_run"))
	cmd.Flags().BoolVar(&confirmed, "confirm", false, opts.t("cli.work.flag.confirm"))
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newWorkExportCommand(opts *runtimeOptions, kind string) *cobra.Command {
	var output string
	var force bool
	cmd := &cobra.Command{Use: "export SLUG", Short: opts.t("cli.work.export.short"), Args: cobra.ExactArgs(1)}
	if kind == "task" {
		cmd.Use = "export TASK_ID"
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		writer := cmd.OutOrStdout()
		var envelope bytes.Buffer
		if output == "-" {
			cmd.SetOut(&envelope)
			defer cmd.SetOut(writer)
		}
		var markdown []byte
		err := runJSON(cmd, func(ctx context.Context) (any, error) {
			var result any
			var err error
			result, markdown, err = exportWorkFile(ctx, opts, kind, args[0], output, force)
			return result, err
		})
		if output != "-" {
			return err
		}
		if err != nil {
			_, _ = writer.Write(envelope.Bytes())
			return err
		}
		_, err = writer.Write(markdown)
		return err
	}
	cmd.Flags().StringVar(&output, "output", "-", opts.t("cli.work.flag.output"))
	cmd.Flags().BoolVar(&force, "force", false, opts.t("cli.work.flag.force"))
	return cmd
}

func exportWorkDocument(ctx context.Context, opts *runtimeOptions, kind, selector string) (domain.WorkDocument, error) {
	input := contract.ExportWorkInput{ProjectSelector: opts.projectSelector()}
	if kind == "task" {
		id, err := parseID(selector, "task id")
		if err != nil {
			return domain.WorkDocument{}, err
		}
		input.TaskID = id
	} else {
		input.Slug = selector
	}
	rt, err := opts.open(ctx, true)
	if err != nil {
		return domain.WorkDocument{}, err
	}
	defer rt.close()
	if kind == "plan" {
		return rt.operationService().ExportPlan(ctx, input)
	}
	return rt.operationService().ExportTask(ctx, input)
}

func writeWorkFile(path string, data []byte, force bool) error {
	if err := paths.ValidateNoSymlinkComponents(path); err != nil {
		return err
	}
	if force {
		return config.WriteAtomic(path, data)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return domain.NewError(domain.ErrValidation, "export destination exists; use --force to replace it", nil)
		}
		return fmt.Errorf("export destination: %w", err)
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	return closeErr
}

func exportWorkFile(ctx context.Context, opts *runtimeOptions, kind, selector, output string, force bool) (any, []byte, error) {
	doc, err := exportWorkDocument(ctx, opts, kind, selector)
	if err != nil {
		return nil, nil, err
	}
	markdown, err := workfile.Encode(doc)
	if err != nil {
		return nil, nil, err
	}
	if output != "-" {
		if err := writeWorkFile(output, markdown, force); err != nil {
			return nil, nil, err
		}
	}
	return map[string]any{"path": output, "type": doc.Type}, markdown, nil
}

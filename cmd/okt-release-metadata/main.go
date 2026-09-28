package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"omakiten/internal/releasemeta"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release metadata:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected create or verify subcommand")
	}
	switch args[0] {
	case "create":
		return runCreate(args[1:])
	case "verify":
		return runVerify(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runCreate(args []string) error {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	dist := flags.String("dist", "dist", "GoReleaser output directory")
	repository := flags.String("repository", "", "GitHub owner/repository")
	tag := flags.String("tag", "", "release tag")
	commit := flags.String("commit", "", "source commit")
	invocationID := flags.String("invocation-id", "", "workflow invocation URI")
	manifestPath := flags.String("manifest", "", "manifest output path")
	provenancePath := flags.String("provenance", "", "in-toto statement output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *manifestPath == "" || *provenancePath == "" {
		return errors.New("manifest and provenance output paths are required")
	}

	manifest, provenance, err := releasemeta.Generate(releasemeta.GenerateOptions{
		Dist:         *dist,
		Repository:   *repository,
		Tag:          *tag,
		Commit:       *commit,
		InvocationID: *invocationID,
	})
	if err != nil {
		return err
	}
	if err := writeJSON(*manifestPath, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := writeJSON(*provenancePath, provenance); err != nil {
		return fmt.Errorf("write provenance: %w", err)
	}
	return nil
}

func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	dist := flags.String("dist", "dist", "GoReleaser output directory")
	repository := flags.String("repository", "", "GitHub owner/repository")
	tag := flags.String("tag", "", "release tag")
	commit := flags.String("commit", "", "source commit")
	manifestPath := flags.String("manifest", "", "manifest path")
	provenanceBundle := flags.String("provenance-bundle", "", "Sigstore provenance bundle path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *manifestPath == "" || *provenanceBundle == "" {
		return errors.New("manifest and provenance-bundle paths are required")
	}
	return releasemeta.Verify(releasemeta.VerifyOptions{
		Dist:             *dist,
		ManifestPath:     *manifestPath,
		ProvenanceBundle: *provenanceBundle,
		Repository:       *repository,
		Tag:              *tag,
		Commit:           *commit,
	})
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(path), data, 0o644)
}

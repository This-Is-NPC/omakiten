package cli

import (
	"bytes"
	"context"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"omakiten/internal/sqlite"
	"omakiten/internal/updater"
)

func TestCLIUpdateBacksUpAndRefreshesTheSelectedProfile(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX candidate executable")
	}
	previous := defaultUpdateClientFactory
	t.Cleanup(func() { defaultUpdateClientFactory = previous })
	for name, refreshFailure := range map[string]bool{"success": false, "refresh failure after swap": true, "project-selected profile": false} {
		t.Run(name, func(t *testing.T) { checkUpdateCommandProfile(t, name, refreshFailure) })
	}
}

func checkUpdateCommandProfile(t *testing.T, name string, refreshFailure bool) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	db, profile := filepath.Join(root, "state.db"), filepath.Join(root, ".omakiten", "config.yaml")
	runCLI(t, db, profile, "init", "--name", "Example", "--slug", "example", "--root", root, "--preset", "omakase")
	binary, trace := filepath.Join(root, "okt"), filepath.Join(root, "candidate-arguments")
	writeFile(t, binary, "original binary")
	t.Setenv("OKT_UPDATE_TRACE", trace)
	exit := "0"
	if refreshFailure {
		exit = "9"
	}
	candidate := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$OKT_UPDATE_TRACE\"\n" +
		"case \"$*\" in\n *refresh-defaults*) printf 'refresh output\\n'; printf 'refresh diagnostic\\n' >&2; exit " + exit + ";;\n" +
		" *) printf '{\"ok\":true}\\n';;\nesac\n"
	archive := tarGzWith(t, map[string][]byte{"okt": []byte(candidate)})
	asset, err := updater.AssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	defaultUpdateClientFactory = func(version string) (updateClient, error) {
		return updateClient{Current: version, BinaryPath: binary, Fetcher: stubFetcher{Tag: "0.32.0"}, Downloader: stubDownloader{Assets: map[string][]byte{asset: archive}}, ReleaseVerifier: acceptingReleaseVerifier()}, nil
	}
	cmd := NewRootCommand("0.31.0")
	var output bytes.Buffer
	cmd.SetOut(&output)
	args := []string{"--db", db, "--config", profile, "update", "--yes"}
	if name == "project-selected profile" {
		t.Chdir(t.TempDir())
		args = []string{"--db", db, "--project", "example", "update", "--yes"}
	}
	cmd.SetArgs(args)
	err = cmd.Execute()
	if (err != nil) != refreshFailure {
		t.Fatalf("update failure=%t: %v, %s", refreshFailure, err, output.String())
	}
	payload := assertUpdateCommandResult(t, output.String(), profile, refreshFailure)
	assertUpdateRecoveryImage(t, payload["backup_path"].(string))
	want := "config\nvalidate\n--config\n" + profile + "\n--config\n" + profile + "\nconfig\nrefresh-defaults\n"
	if got := readFile(t, trace); got != want {
		t.Fatalf("selected profile lost: %q, want %q", got, want)
	}
	if got := readFile(t, binary); got != candidate {
		t.Fatal("new binary was not retained")
	}
}

func assertUpdateCommandResult(t *testing.T, output, profile string, refreshFailure bool) map[string]any {
	t.Helper()
	envelope := decodeEnvelope(t, output)
	key := "data"
	if refreshFailure {
		key = "details"
	}
	payload := envelope[key].(map[string]any)
	if payload["applied"] != true {
		t.Fatalf("durable swap missing: %v", payload)
	}
	if refreshFailure && (!strings.Contains(envelope["msg"].(string), "refresh diagnostic") || payload["manual_command"] != updateDefaultsManualCommandForConfig(profile)) {
		t.Fatalf("refresh recovery: %v", envelope)
	}
	return payload
}

func assertUpdateRecoveryImage(t *testing.T, path string) {
	t.Helper()
	backup, err := sqlite.OpenCurrentReadOnly(context.Background(), path)
	if err != nil {
		t.Fatalf("recovery image: %v", err)
	}
	defer func() { _ = backup.Close() }()
	if _, err := backup.FindProjectBySlug(context.Background(), "example"); err != nil {
		t.Fatalf("project absent from backup: %v", err)
	}
}

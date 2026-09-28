package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInteractiveSetupConfirmsOrCancelsBeforeWriting(t *testing.T) {
	for name, keys := range map[string]string{"confirm": "\r\t", "cancel": "\x03"} {
		t.Run(name, func(t *testing.T) {
			checkInteractiveSetup(t, name, keys)
		})
	}
}

func checkInteractiveSetup(t *testing.T, name, keys string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("OMAKITEN_HOME", root)
	inputs := setupInputs{CLILang: "en", TUILang: "en", AgentLang: "en", Preset: "omakase"}
	driveTerminal(t, []terminalReply{{"Install the Omakiten skill", keys}}, func(ctx context.Context) {
		selected, err := runSetupPicker(ctx, inputs, pickerNeeds{Harness: true})
		if name == "cancel" {
			if err == nil {
				t.Fatal("cancel accepted")
			}
			return
		}
		if err != nil || len(selected.Harnesses) != 1 || selected.Harnesses[0] != "agents" {
			t.Fatalf("selection: %+v, %v", selected, err)
		}
	})
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("picker wrote configuration: %v, %v", entries, err)
	}
}

func TestCLIInteractiveUninstallHonorsPurgeSelectionAndCancellation(t *testing.T) {
	for name, keys := range map[string]string{"keep config": "\x1b[B\x1b[B\ry", "cancel": "\x03"} {
		t.Run(name, func(t *testing.T) {
			checkInteractiveUninstall(t, name, keys)
		})
	}
}

func checkInteractiveUninstall(t *testing.T, name, keys string) {
	t.Helper()
	home := seedFakeInstall(t)
	data := filepath.Join(home, ".data", "omakiten")
	cfg := filepath.Join(home, ".cfg", "omakiten")
	binary := filepath.Join(home, ".local", "bin", "okt")
	before := readFile(t, binary)
	driveTerminal(t, []terminalReply{{"Uninstall okt", keys}}, func(ctx context.Context) {
		selected, err := resolveUninstallInputs(ctx, uninstallInputs{}, false)
		if name == "cancel" {
			if err == nil {
				t.Fatal("cancel accepted")
			}
			return
		}
		if err != nil || !selected.PurgeData || selected.PurgeConfig {
			t.Fatalf("purge selection: %+v, %v", selected, err)
		}
		if _, err := runUninstall(ctx, selected); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("unselected configuration lost: %v", err)
	}
	if name == "cancel" {
		readBackEquals(t, binary, before)
		if _, err := os.Stat(data); err != nil {
			t.Fatal(err)
		}
	} else if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("selected data retained: %v", err)
	}
}

func TestCLIInteractiveUpdateRequiresConsent(t *testing.T) {
	for name, key := range map[string]string{"accept": "y", "decline": "n"} {
		t.Run(name, func(t *testing.T) {
			output := driveTerminal(t, []terminalReply{{"y apply · n cancel · ctrl+c quit", key}}, func(ctx context.Context) {
				err := confirmUpdate(ctx, "0.31.0", "0.32.0", false)
				if (err != nil) != (name == "decline") {
					t.Fatalf("confirmation: %v", err)
				}
			})
			if !strings.Contains(output, "0.31.0") || !strings.Contains(output, "0.32.0") {
				t.Fatalf("version pair missing: %q", output)
			}
		})
	}
}

func TestCLIInteractiveProjectDeletionRequiresConsent(t *testing.T) {
	for name, reply := range map[string]string{"accept": "y\n", "decline": "no\n"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
			db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
			runCLI(t, db, cfg, "init", "--name", "Example", "--slug", "example")
			runCLI(t, db, cfg, "task", "create", "--confirm", "-t", "Retain on cancellation")
			driveTerminal(t, []terminalReply{{"[y/N]", reply}}, func(context.Context) {
				cmd := NewRootCommand("test")
				cmd.SetArgs([]string{"--db", db, "--config", cfg, "projects", "delete", "example"})
				err := cmd.Execute()
				if (err != nil) != (name == "decline") {
					t.Fatalf("deletion: %v", err)
				}
			})
			if name == "decline" {
				out := runCLI(t, db, cfg, "projects", "list")
				if !strings.Contains(out, "example") {
					t.Fatal("declined project deleted")
				}
			} else {
				runCLIExpectError(t, db, cfg, "project_not_found", "projects", "delete", "example", "--yes")
			}
		})
	}
}

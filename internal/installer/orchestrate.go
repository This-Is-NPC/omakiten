package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"omakiten/internal/agentsetup"
	"omakiten/internal/paths"
)

// WriteActivePreset records the chosen preset in <configDir>/.active so
// the next CLI invocation resolves to it. Mirrors install.sh's
// write_active_preset: creates the config dir if missing and writes
// "<preset>.yaml\n" via paths.SetActiveConfigInDir (which handles the
// basename guard + trailing newline).
//
// configDir is resolved from paths.ConfigDir() — the same precedence
// chain the rest of the runtime uses (OMAKITEN_HOME → XDG → ~/.config).
// Returns the resolved dir so the caller can echo it in the
// `cli.setup.status.preset_written` message.
func WriteActivePreset(preset string) (configDir string, err error) {
	preset = strings.TrimSpace(preset)
	if preset == "" {
		return "", errors.New("installer: preset name is required")
	}
	dir, err := paths.ConfigDir()
	if err != nil {
		return "", err
	}
	if err := paths.SetActiveConfigInDir(dir, preset+".yaml"); err != nil {
		return "", err
	}
	return dir, nil
}

// WrapperTargets is the set of rc files the installer considers writing
// the okt() wrapper into. Order is deterministic so test assertions
// against the "installed into" log line stay stable. The bash installer
// touches .bashrc + .zshrc only when they already exist OR when the
// invoking shell matches; WriteWrappers takes the simpler stance
// (touch only when the file exists) because the Go path runs from the
// post-binary-install context where SHELL detection is unreliable
// (curl|bash already changed the parent shell).
//
// Each entry is an absolute path. Callers pass HOME explicitly so tests
// can pin a tmpdir without env-var stomping.
func WrapperTargets(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
	}
}

// WriteWrappers calls InstallWrapper for every rc file in
// WrapperTargets(home) that exists on disk. Returns the slice of rc
// paths the wrapper landed in so the caller can render the
// `cli.setup.status.wrapper_written` line with the same list bash
// echoed via `installed_into[*]`.
//
// Skipped rc files (missing) are not an error — the installer is fine
// with a user who only runs one shell. When the slice is empty the
// caller prints `cli.setup.status.wrapper_skipped` instead.
func WriteWrappers(home string) ([]string, error) {
	var installed []string
	for _, rc := range WrapperTargets(home) {
		if _, err := os.Stat(rc); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", rc, err)
		}
		if err := InstallWrapper(rc); err != nil {
			return nil, err
		}
		installed = append(installed, rc)
	}
	return installed, nil
}

// HarnessSetupResult records one harness-configuration attempt so the
// caller can render success/failure lines through the catalog without
// re-running the work. Status mirrors agentsetup.Result.Status verbatim
// for the success path; on failure ExitCode is non-zero and Err carries
// the underlying error.
type HarnessSetupResult struct {
	Harness  string
	Status   string
	ExitCode int
	Err      error
}

// SetupHarnesses invokes `<oktBin> mcp setup --harness <name> --force`
// for each entry in names, capturing per-harness outcomes so the
// caller can render the cli.setup.status.harness_configured /
// cli.setup.status.harness_failed lines without aborting the loop on
// the first failure — install.sh keeps going on per-harness errors
// because one missing config dir (e.g. user never installed Crush)
// shouldn't block configuring the rest.
//
// oktBin must be an absolute path to the okt binary (typically
// $INSTALL_DIR/okt). The caller resolves the binary path; passing a
// bare "okt" would rely on $PATH and surprise users whose shell hasn't
// re-sourced the wrapper yet.
//
// SetupHarnesses validates each harness name against
// agentsetup.SupportedHarnesses before invoking — an unknown name
// short-circuits with a non-zero ExitCode and an Err so the caller
// renders the same warning bash does for unknown harnesses.
func SetupHarnesses(ctx context.Context, oktBin string, names []string) []HarnessSetupResult {
	results := make([]HarnessSetupResult, 0, len(names))
	supported := agentsetup.SupportedHarnesses()
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !contains(supported, name) {
			results = append(results, HarnessSetupResult{
				Harness:  name,
				Status:   "unsupported",
				ExitCode: 1,
				Err:      fmt.Errorf("unsupported harness %q", name),
			})
			continue
		}
		cmd := exec.CommandContext(ctx, oktBin, "mcp", "setup", "--harness", name, "--force")
		// Discard child stdout/stderr — install.sh did the same with
		// `>/dev/null 2>&1`. The summary the caller renders carries
		// the exit code, which is sufficient for the "re-run manually"
		// hint we surface on failure.
		cmd.Stdout = nil
		cmd.Stderr = nil
		err := cmd.Run()
		res := HarnessSetupResult{Harness: name}
		if err == nil {
			res.Status = "ok"
			res.ExitCode = 0
			results = append(results, res)
			continue
		}
		res.Status = "failed"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = 1
		}
		res.Err = err
		results = append(results, res)
	}
	return results
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

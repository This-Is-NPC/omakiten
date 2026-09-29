package installscript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/testfixtures"
)

func TestInstallerLocalKeepsSetupAndRegistrationInOneScope(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Bash is not installed")
	}
	binary := filepath.Join(t.TempDir(), "okt")
	gitConfig, err := testfixtures.PresetGitConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	build := exec.Command("go", "build", "-o", binary, "./cmd/okt")
	build.Dir = repoRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build installer fixture: %v\n%s", err, output)
	}
	for name, args := range map[string][]string{"silent": {"--silent"}, "selected preset": nil} {
		t.Run(name, func(t *testing.T) { checkLocalInstallationScope(t, binary, args) })
	}
}

func checkLocalInstallationScope(t *testing.T, binary string, args []string) {
	t.Helper()
	home := t.TempDir()
	parent := filepath.Join(home, "Projects")
	repository := filepath.Join(parent, "omakiten")
	for _, relative := range []string{"scripts/install-local.sh", "scripts/lib/workspace.sh"} {
		body, err := os.ReadFile(filepath.Join(repoRoot(t), relative))
		if err != nil {
			t.Fatal(err)
		}
		writeCheckFixture(t, filepath.Join(repository, relative), string(body))
	}
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	writeCheckFixture(t, filepath.Join(repository, ".tmp", "build", "okt"), string(body))
	localConfig := filepath.Join(parent, ".omakiten", "config", "omakase.yaml")
	writeCheckFixture(t, localConfig, "config: {unsupported_field: true}\n")
	writeCheckFixture(t, filepath.Join(home, ".bashrc"), "# user shell configuration\n")
	environment := append(os.Environ(), "HOME="+home, "USERPROFILE="+home,
		"OMAKITEN_HOME="+filepath.Join(home, "another-install"), "XDG_DATA_HOME="+filepath.Join(home, "data"),
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_STATE_HOME="+filepath.Join(home, "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"MISE_PROJECT_ROOT="+repository, "OKT_PRESET=omakase", "OKT_CLI_LANG=en", "OKT_TUI_LANG=en",
		"OKT_AGENT_LANG=en", "OKT_HARNESSES=0")
	install := exec.Command("bash", append([]string{filepath.Join(repository, "scripts", "install-local.sh")}, args...)...)
	install.Env, install.Dir = environment, repository
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	global := filepath.Join(home, ".config", "omakiten", "config.yaml")
	installed := filepath.Join(home, ".local", "bin", "okt")
	command := exec.Command(installed, "--config", global, "--project", "omakiten", "project", "resume")
	command.Env, command.Dir = append(environment, "OMAKITEN_HOME="), repository
	if output, err := command.CombinedOutput(); err != nil || !strings.Contains(string(output), `"slug":"omakiten"`) {
		t.Fatalf("registered project: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "omakiten", "omakiten.db")); err != nil {
		t.Fatalf("user database absent: %v", err)
	}
	unchanged, err := os.ReadFile(localConfig)
	if err != nil || string(unchanged) != "config: {unsupported_field: true}\n" {
		t.Fatalf("parent configuration changed: %v, %s", err, unchanged)
	}
}

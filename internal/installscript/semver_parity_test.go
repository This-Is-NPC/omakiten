package installscript

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerSemVerParity(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0"}
	root := repoRoot(t)
	t.Run("bash", func(t *testing.T) { testBashSemVerParity(t, root, ordered) })
	t.Run("powershell", func(t *testing.T) { testPowerShellSemVerParity(t, root, ordered) })
}

func testBashSemVerParity(t *testing.T, root string, ordered []string) {
	requireBash(t)
	for i := 0; i < len(ordered)-1; i++ {
		command := fmt.Sprintf("source %q; version_compare %q %q", filepath.Join(root, "install.sh"), ordered[i], ordered[i+1])
		cmd := exec.Command("bash", "-c", command)
		cmd.Env = append(os.Environ(), "OKT_INSTALLER_TEST_LIBRARY=1")
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "-1" {
			t.Fatalf("%s < %s: output=%q err=%v", ordered[i], ordered[i+1], out, err)
		}
	}
	for _, invalid := range []string{"1.0.0-01", "1.0.0-alpha..1", "01.0.0", "1.0.0-"} {
		command := fmt.Sprintf("source %q; is_release_version %q", filepath.Join(root, "install.sh"), invalid)
		cmd := exec.Command("bash", "-c", command)
		cmd.Env = append(os.Environ(), "OKT_INSTALLER_TEST_LIBRARY=1")
		if err := cmd.Run(); err == nil {
			t.Errorf("Bash accepted invalid SemVer %q", invalid)
		}
	}
}

func testPowerShellSemVerParity(t *testing.T, root string, ordered []string) {
	pwsh := resolvePwsh(t)
	for i := 0; i < len(ordered)-1; i++ {
		command := fmt.Sprintf("$env:OKT_INSTALLER_TEST_LIBRARY='1'; $env:INSTALL_DIR='%s'; . '%s'; Compare-ReleaseVersion '%s' '%s'", t.TempDir(), filepath.Join(root, "install.ps1"), ordered[i], ordered[i+1])
		out, err := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "-1" {
			t.Fatalf("%s < %s: output=%q err=%v", ordered[i], ordered[i+1], out, err)
		}
	}
	for _, invalid := range []string{"1.0.0-01", "1.0.0-alpha..1", "01.0.0", "1.0.0-"} {
		command := fmt.Sprintf("$env:OKT_INSTALLER_TEST_LIBRARY='1'; $env:INSTALL_DIR='%s'; . '%s'; Assert-ReleaseVersionShape '%s'", t.TempDir(), filepath.Join(root, "install.ps1"), invalid)
		if err := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command", command).Run(); err == nil {
			t.Errorf("PowerShell accepted invalid SemVer %q", invalid)
		}
	}
}

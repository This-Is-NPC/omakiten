package arch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

type scriptTask struct {
	Description string
	Run         any
	Depends     []string
}

func TestMiseTasksDelegateToExecutableScripts(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, ".mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Tools map[string]string
		Tasks map[string]scriptTask
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for tool, version := range config.Tools {
		if version == "latest" || !strings.ContainsAny(version, "0123456789") {
			t.Errorf("tool %s must pin a version: %q", tool, version)
		}
	}
	for name, task := range config.Tasks {
		t.Run(name, func(t *testing.T) { assertScriptTask(t, root, task) })
	}
}

func assertScriptTask(t *testing.T, root string, task scriptTask) {
	t.Helper()
	if task.Description == "" {
		t.Error("task needs a description")
	}
	switch run := task.Run.(type) {
	case string:
		assertTaskScript(t, root, run)
	case []any:
		for _, command := range run {
			value, ok := command.(string)
			if !ok {
				t.Fatalf("task command is not a string: %v", command)
			}
			assertTaskScript(t, root, value)
		}
	case nil:
		if len(task.Depends) == 0 {
			t.Fatal("task must run scripts or depend on other tasks")
		}
	default:
		t.Fatalf("unsupported task command: %v", run)
	}
}

func assertTaskScript(t *testing.T, root, command string) {
	t.Helper()
	fields := strings.Fields(command)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "scripts/") || strings.ContainsAny(command, "\n\r;|&`$") {
		t.Fatalf("task contains inline work instead of a script invocation: %q", command)
	}
	path := filepath.Join(root, fields[0])
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("task script is missing: %s (%v)", path, err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatalf("task script is not executable: %s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(body), "#!") {
		t.Fatalf("task script needs a shebang: %s (%v)", path, err)
	}
}

package cli

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"omakiten/internal/agentruntime"
	"omakiten/internal/paths"
	"omakiten/internal/syncserver"
)

const syncTestToken = "sync-test-token"

// syncMachine is one local store plus the directory its project is rooted in.
type syncMachine struct {
	db   string
	root string
}

func newSyncMachine(t *testing.T, name string) syncMachine {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, name)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return syncMachine{db: filepath.Join(tmp, name+".db"), root: root}
}

func (m syncMachine) run(t *testing.T, args ...string) map[string]any {
	t.Helper()
	return decodeEnvelope(t, runCLI(t, m.db, "", args...))["data"].(map[string]any)
}

type syncedTask struct{ Title, Bucket string }

func (m syncMachine) tasks(t *testing.T) []syncedTask {
	t.Helper()
	var out []syncedTask
	for _, raw := range m.run(t, "--project", "shared", "list")["tasks"].([]any) {
		task := raw.(map[string]any)
		out = append(out, syncedTask{task["title"].(string), task["bucket_key"].(string)})
	}
	return out
}

func (m syncMachine) taskID(t *testing.T, title string) string {
	t.Helper()
	for _, raw := range m.run(t, "--project", "shared", "list")["tasks"].([]any) {
		task := raw.(map[string]any)
		if task["title"] == title {
			return strconv.Itoa(int(task["id"].(float64)))
		}
	}
	t.Fatalf("no task %q", title)
	return ""
}

func startSyncServer(t *testing.T) string {
	t.Helper()
	store, err := syncserver.Open(context.Background(), filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server := httptest.NewServer(syncserver.NewHandler(store, []string{syncTestToken}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestCLISyncTwoMachinesConverge(t *testing.T) {
	t.Setenv(paths.HomeEnv, filepath.Join(t.TempDir(), "home"))
	t.Setenv(agentruntime.SyncTokenEnv, syncTestToken)
	url := startSyncServer(t)
	a, b := newSyncMachine(t, "a"), newSyncMachine(t, "b")

	t.Chdir(a.root)
	a.run(t, "init", "--name", "Shared", "--slug", "shared")
	a.run(t, "project", "edit", "--sync-scope", "synced")
	a.run(t, "task", "create", "--title", "Write parser")
	a.run(t, "task", "create", "--title", "Deploy release")
	a.run(t, "comment", "add", a.taskID(t, "Write parser"), "-b", "a note")
	if got := a.run(t, "remote", "set", url)["url"]; got != url {
		t.Fatalf("remote set url = %v", got)
	}
	if got := a.run(t, "remote", "show")["url"]; got != url {
		t.Fatalf("remote show url = %v", got)
	}
	report := a.run(t, "sync")["projects"].([]any)
	if len(report) != 1 || report[0].(map[string]any)["slug"] != "shared" || report[0].(map[string]any)["pushed"].(float64) == 0 {
		t.Fatalf("sync report = %v", report)
	}

	t.Chdir(b.root)
	b.run(t, "remote", "set", url)
	remote := b.run(t, "remote", "projects")["projects"].([]any)
	if len(remote) != 1 {
		t.Fatalf("remote projects = %v", remote)
	}
	uid := remote[0].(map[string]any)["uid"].(string)
	cloned := b.run(t, "projects", "clone", uid, "--root", b.root)["project"].(map[string]any)
	if cloned["slug"] != "shared" || cloned["root_path"] != b.root {
		t.Fatalf("cloned project = %v", cloned)
	}
	if got, want := b.tasks(t), a.tasks(t); len(want) != 2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("tasks on B = %v, on A = %v", got, want)
	}

	b.run(t, "edit", b.taskID(t, "Deploy release"), "--title", "Ship release notes")
	b.run(t, "sync")
	a.run(t, "sync")
	if got, want := a.tasks(t), b.tasks(t); !reflect.DeepEqual(got, want) || !strings.Contains(got[0].Title+got[1].Title, "Ship release notes") {
		t.Fatalf("tasks on A = %v, on B = %v", got, want)
	}
}

func TestCLISyncProjectSelectsOneProject(t *testing.T) {
	t.Setenv(paths.HomeEnv, filepath.Join(t.TempDir(), "home"))
	t.Setenv(agentruntime.SyncTokenEnv, syncTestToken)
	url := startSyncServer(t)
	a := newSyncMachine(t, "a")
	t.Chdir(a.root)
	a.run(t, "init", "--name", "Shared", "--slug", "shared")
	a.run(t, "project", "edit", "--sync-scope", "synced")
	a.run(t, "remote", "set", url)
	report := a.run(t, "--project", "shared", "sync")["projects"].([]any)
	if len(report) != 1 {
		t.Fatalf("report = %v", report)
	}
	runCLIExpectError(t, a.db, "", "project_not_found", "--project", "missing", "sync")
}

func TestCLISyncFailures(t *testing.T) {
	t.Setenv(paths.HomeEnv, filepath.Join(t.TempDir(), "home"))
	t.Setenv(agentruntime.SyncTokenEnv, "")
	url := startSyncServer(t)
	a := newSyncMachine(t, "a")
	t.Chdir(a.root)
	a.run(t, "init", "--name", "Shared", "--slug", "shared")

	noRemote := runCLIExpectError(t, a.db, "", "validation_error", "sync")
	if !strings.Contains(noRemote["msg"].(string), "okt remote set") {
		t.Fatalf("no-remote error = %v", noRemote)
	}
	runCLIExpectError(t, a.db, "", "validation_error", "remote", "set", "not-a-url")

	a.run(t, "remote", "set", url)
	noToken := runCLIExpectError(t, a.db, "", "validation_error", "remote", "projects")
	if !strings.Contains(noToken["msg"].(string), agentruntime.SyncTokenEnv) {
		t.Fatalf("no-token error = %v", noToken)
	}

	data, err := paths.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(data, "sync.token")
	if err := os.WriteFile(tokenFile, []byte(syncTestToken+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loose := runCLIExpectError(t, a.db, "", "validation_error", "remote", "projects")
	if !strings.Contains(loose["msg"].(string), "readable by group or others") {
		t.Fatalf("loose-permission error = %v", loose)
	}
	if err := os.Chmod(tokenFile, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.run(t, "remote", "projects")["projects"]; got == nil {
		t.Fatalf("remote projects with token file = %v", got)
	}
}

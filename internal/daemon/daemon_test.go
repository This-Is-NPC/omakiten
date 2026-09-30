package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omakiten/internal/agentruntime"
	"omakiten/internal/contract"
	"omakiten/internal/paths"
)

type daemonFixture struct {
	dbPath     string
	configPath string
	root       string
}

func newDaemonFixture(t *testing.T) daemonFixture {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv(paths.HomeEnv, filepath.Join(tmp, "home"))
	// The repo-local walk stops at HOME; pin it so installs above the temp
	// directory cannot leak into the fixture.
	t.Setenv("HOME", tmp)
	fixture := daemonFixture{
		dbPath:     filepath.Join(tmp, "data", "omakiten.db"),
		configPath: filepath.Join(tmp, "config", "omakase.yaml"),
		root:       filepath.Join(tmp, "alpha"),
	}
	if err := os.MkdirAll(fixture.root, 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f daemonFixture) open(t *testing.T) *agentruntime.Runtime {
	t.Helper()
	rt, err := agentruntime.Open(context.Background(), agentruntime.Options{DBPath: f.dbPath, ConfigPath: f.configPath, CWD: filepath.Dir(f.root)})
	if err != nil {
		t.Fatalf("agentruntime.Open: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func session(rt *agentruntime.Runtime) agentruntime.Session {
	return agentruntime.Session{Store: rt.Store(), Cache: rt.Cache(), ConfigPath: rt.ConfigPath(), DBPath: rt.DBPath(), Version: "test", Snapshot: rt.Snapshot()}
}

// startDaemon runs Run until the test ends and returns its discovery document and token.
func startDaemon(t *testing.T, rt *agentruntime.Runtime) (Discovery, string, <-chan error) {
	t.Helper()
	return startSession(t, session(rt))
}

func startSession(t *testing.T, s agentruntime.Session) (Discovery, string, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, s, contract.ServeOptions{Addr: "127.0.0.1:0", Poll: 20 * time.Millisecond})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	dir, err := paths.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if discovery, err := readDiscovery(dir); err == nil {
			token, err := os.ReadFile(discovery.TokenFile)
			if err != nil {
				t.Fatal(err)
			}
			return discovery, string(token), done
		}
		select {
		case err := <-done:
			t.Fatalf("Run exited early: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("daemon never published serve.json")
	return Discovery{}, "", nil
}

func request(t *testing.T, method, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return resp.StatusCode, out
}

func TestDaemonServesProjectsAndStreamsWritesFromOtherProcesses(t *testing.T) {
	fixture := newDaemonFixture(t)
	daemonRuntime := fixture.open(t)
	project, err := agentruntime.InitProject(context.Background(), daemonRuntime.Store(), "Alpha", "alpha", fixture.root)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	discovery, token, _ := startDaemon(t, daemonRuntime)

	status, body := request(t, http.MethodGet, discovery.URL+"/api/v1/projects", token, "")
	if status != http.StatusOK || !strings.Contains(toJSON(body), `"slug":"alpha"`) {
		t.Fatalf("projects = %d %v", status, body)
	}
	if status, _ := request(t, http.MethodGet, discovery.URL+"/api/v1/projects", "wrong", ""); status != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", status)
	}

	stream := openStream(t, discovery.URL+"/api/v1/events?project=alpha", token)

	cliRuntime := fixture.open(t)
	created, err := cliRuntime.Service().ForCLI().CreateTask(context.Background(), contract.CreateTaskInput{
		ProjectSelector: contract.ProjectSelector{ProjectID: project.ID},
		Title:           "Written by another process",
		Description:     "The daemon must stream this.",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if message := waitForEvent(t, stream, "task.created"); !strings.Contains(message, `"project_id":`+itoa(project.ID)) {
		t.Fatalf("task.created message = %s", message)
	}

	status, body = request(t, http.MethodGet, discovery.URL+"/api/v1/projects/alpha/tasks/"+itoa(created.Task.ID), token, "")
	if status != http.StatusOK || !strings.Contains(toJSON(body), "Written by another process") {
		t.Fatalf("task.show = %d %v", status, body)
	}
	status, body = request(t, http.MethodPost, discovery.URL+"/api/v1/projects/alpha/tasks/"+itoa(created.Task.ID)+"/comments", token, `{"body":"from the GUI"}`)
	if status != http.StatusOK {
		t.Fatalf("add comment = %d %v", status, body)
	}
	waitForEvent(t, stream, "comment")
}

func TestDaemonKeepsTheSessionRuntimeForTheSessionProject(t *testing.T) {
	fixture := newDaemonFixture(t)
	rt := fixture.open(t)
	project, err := agentruntime.InitProject(context.Background(), rt.Store(), "Alpha", "alpha", fixture.root)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	// A stale repo-local install at the project root must not replace the
	// workflow the session was opened with.
	if err := os.MkdirAll(filepath.Join(fixture.root, ".omakiten"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, ".omakiten", "config.yaml"), []byte("preset: missing.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := session(rt)
	s.Project = project.Context()
	discovery, token, _ := startSession(t, s)

	if status, body := request(t, http.MethodGet, discovery.URL+"/api/v1/projects/alpha/tasks", token, ""); status != http.StatusOK {
		t.Fatalf("session project tasks = %d %v", status, body)
	}
}

func TestDaemonRefusesSecondInstanceAndCleansUp(t *testing.T) {
	fixture := newDaemonFixture(t)
	rt := fixture.open(t)
	discovery, _, _ := startDaemon(t, rt)

	err := Run(context.Background(), session(rt), contract.ServeOptions{Addr: "127.0.0.1:0", Poll: time.Second})
	if err == nil || !strings.Contains(toJSON(err), discovery.URL) {
		t.Fatalf("second Run = %v, want already running with %s", err, discovery.URL)
	}
	if err := Run(context.Background(), session(rt), contract.ServeOptions{Addr: "0.0.0.0:0", Poll: time.Second}); err == nil {
		t.Fatal("Run on a non-loopback address succeeded")
	}
}

func TestDaemonRemovesDiscoveryOnShutdown(t *testing.T) {
	fixture := newDaemonFixture(t)
	rt := fixture.open(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, session(rt), contract.ServeOptions{Addr: "127.0.0.1:0", Poll: time.Second}) }()
	dir, _ := paths.StateDir()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := readDiscovery(dir); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon never published serve.json")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run after cancel = %v", err)
	}
	for _, name := range []string{discoveryFile, tokenFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left behind: %v", name, err)
		}
	}
}

func openStream(t *testing.T, url, token string) *bufio.Reader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream = %d %s", resp.StatusCode, raw)
	}
	return bufio.NewReader(resp.Body)
}

// waitForEvent returns the data line of the first message of eventType.
func waitForEvent(t *testing.T, stream *bufio.Reader, eventType string) string {
	t.Helper()
	found := make(chan string, 1)
	go func() {
		current := ""
		for {
			line, err := stream.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			if value, ok := strings.CutPrefix(line, "event: "); ok {
				current = value
			}
			if value, ok := strings.CutPrefix(line, "data: "); ok && current == eventType {
				found <- value
				return
			}
		}
	}()
	select {
	case data := <-found:
		return data
	case <-time.After(10 * time.Second):
		t.Fatalf("no %s event within 10s", eventType)
		return ""
	}
}

func toJSON(value any) string {
	if err, ok := value.(error); ok {
		value = map[string]any{"error": err.Error(), "detail": err}
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func itoa(id int64) string {
	raw, _ := json.Marshal(id)
	return string(raw)
}

package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"omakiten/internal/agentruntime"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
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
	return startWithStderr(t, s, nil)
}

func startWithStderr(t *testing.T, s agentruntime.Session, stderr io.Writer) (Discovery, string, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, s, contract.ServeOptions{Addr: "127.0.0.1:0", Poll: 20 * time.Millisecond, Stderr: stderr})
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
	project := registerProject(t, daemonRuntime, "alpha", fixture.root)
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
	project := registerProject(t, rt, "alpha", fixture.root)
	// A stale repo-local install at the project root must not replace the
	// workflow the session was opened with.
	writeRepoLocalConfig(t, fixture.root, "preset: missing.yaml\n")
	s := session(rt)
	s.Project = project.Context()
	discovery, token, _ := startSession(t, s)

	if status, body := request(t, http.MethodGet, discovery.URL+"/api/v1/projects/alpha/tasks", token, ""); status != http.StatusOK {
		t.Fatalf("session project tasks = %d %v", status, body)
	}
}

// TestDaemonResumesStreamAfterReconnect: a client that reconnects with
// Last-Event-ID receives the events committed while it was away.
func TestDaemonResumesStreamAfterReconnect(t *testing.T) {
	fixture := newDaemonFixture(t)
	rt := fixture.open(t)
	project := registerProject(t, rt, "alpha", fixture.root)
	stderr := &lockedBuffer{}
	discovery, token, _ := startWithStderr(t, session(rt), stderr)
	waitForLine(t, stderr, discovery.URL, discoveryFile)

	seen, err := rt.Store().LatestEventID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Service().ForCLI().CreateTask(context.Background(), contract.CreateTaskInput{
		ProjectSelector: contract.ProjectSelector{ProjectID: project.ID},
		Title:           "Committed while disconnected",
		Description:     "Replay must deliver this.",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	stream := openStreamFrom(t, discovery.URL+"/api/v1/events?project=alpha", token, itoa(seen))
	if data := waitForEvent(t, stream, "task.created"); !strings.Contains(data, `"project_id":`+itoa(project.ID)) {
		t.Fatalf("replayed task.created = %s", data)
	}
}

// waitForLine waits until the startup line names every part.
func waitForLine(t *testing.T, stderr *lockedBuffer, parts ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		line := stderr.String()
		missing := slices.ContainsFunc(parts, func(part string) bool { return !strings.Contains(line, part) })
		if !missing {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("startup line = %q, want %v", line, parts)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// lockedBuffer is an io.Writer the daemon goroutine and the test share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestDaemonResolvesEachProjectWorkflowIndependently: a project whose own
// repo-local install is broken fails alone; the others keep answering.
func TestDaemonResolvesEachProjectWorkflowIndependently(t *testing.T) {
	fixture := newDaemonFixture(t)
	rt := fixture.open(t)
	registerProject(t, rt, "alpha", fixture.root)
	broken := filepath.Join(filepath.Dir(fixture.root), "broken")
	registerProject(t, rt, "broken", broken)
	writeRepoLocalConfig(t, broken, "preset: missing.yaml\n")
	discovery, token, _ := startDaemon(t, rt)

	cases := map[string]struct {
		status int
		code   string
	}{
		"alpha":  {http.StatusOK, ""},
		"broken": {http.StatusInternalServerError, "config_invalid"},
		"ghost":  {http.StatusNotFound, "project_not_found"},
	}
	for slug, want := range cases {
		status, body := request(t, http.MethodGet, discovery.URL+"/api/v1/projects/"+slug+"/tasks", token, "")
		if status != want.status || (want.code != "" && body["code"] != want.code) {
			t.Errorf("%s = %d %v, want %d %s", slug, status, body, want.status, want.code)
		}
	}
}

// TestDaemonFromRepoLocalInstallRefusesProjectsWithoutOne: the session's
// repo-local workflow belongs to its own project, so another project
// without an install gets config_invalid instead of a foreign policy.
func TestDaemonFromRepoLocalInstallRefusesProjectsWithoutOne(t *testing.T) {
	fixture := newDaemonFixture(t)
	rt := fixture.open(t)
	registerProject(t, rt, "alpha", fixture.root)
	s := session(rt)
	s.RepoLocalDir = filepath.Join(filepath.Dir(fixture.root), "elsewhere", ".omakiten")
	discovery, token, _ := startSession(t, s)

	status, body := request(t, http.MethodGet, discovery.URL+"/api/v1/projects/alpha/tasks", token, "")
	if status != http.StatusInternalServerError || body["code"] != "config_invalid" || !strings.Contains(toJSON(body), "outside a repo-local install") {
		t.Fatalf("alpha = %d %v, want config_invalid with guidance", status, body)
	}
}

func registerProject(t *testing.T, rt *agentruntime.Runtime, slug, root string) domain.Project {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	project, err := agentruntime.InitProject(context.Background(), rt.Store(), slug, slug, root)
	if err != nil {
		t.Fatalf("InitProject(%s): %v", slug, err)
	}
	return project
}

func writeRepoLocalConfig(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".omakiten")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
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
	for _, addr := range []string{"0.0.0.0:0", "example.com:80", "7766"} {
		if err := Run(context.Background(), session(rt), contract.ServeOptions{Addr: addr, Poll: time.Second}); err == nil {
			t.Errorf("Run on %q succeeded, want a loopback address error", addr)
		}
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
	return openStreamFrom(t, url, token, "")
}

func openStreamFrom(t *testing.T, url, token, lastEventID string) *bufio.Reader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
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

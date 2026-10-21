package agentlaunch

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"omakiten/internal/domain"
)

func args(values ...any) []domain.AgentArg {
	var out []domain.AgentArg
	for _, v := range values {
		switch v := v.(type) {
		case string:
			out = append(out, domain.AgentArg{Values: []string{v}})
		case []string:
			out = append(out, domain.AgentArg{Values: v, Group: true})
		}
	}
	return out
}

func TestExpandDropsGroupsWithEmptyPlaceholders(t *testing.T) {
	argv := args("codex", "exec", []string{"--model", "{model}"}, "--cd", "{cwd}")
	got := Expand(argv, map[string]string{"model": "", "cwd": "/repo"})
	want := []string{"codex", "exec", "--cd", "/repo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Expand() = %q, want %q", got, want)
	}
	got = Expand(argv, map[string]string{"model": "o4", "cwd": "{model}"})
	if got[2] != "--model" || got[3] != "o4" || got[5] != "{model}" {
		t.Fatalf("Expand() = %q, want the group kept and values not re-expanded", got)
	}
}

func testAdapters() domain.AgentAdapters {
	return domain.AgentAdapters{
		Defaults: domain.AgentDefaults{Harness: "codex", Model: "o4"},
		Fallback: domain.AgentDefaults{Harness: "claude-code", Model: "haiku"},
		Harnesses: map[string]domain.HarnessSpec{
			"codex":       {Argv: args("codex", "exec", []string{"--model", "{model}"}, "--", "{prompt}")},
			"claude-code": {Argv: args("claude", "-p", []string{"--model", "{model}"}, "--", "{prompt}")},
		},
	}
}

// fakeLauncher reports installed as the executables on PATH and records
// every detached start.
func fakeLauncher(installed ...string) (Launcher, *[][]string) {
	var starts [][]string
	return Launcher{
		LookPath: func(name string) (string, error) {
			if slices.Contains(installed, name) {
				return "/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Detach: func(argv, env []string, dir, output string) error {
			starts = append(starts, append(append(argv, "DIR="+dir, "OUTPUT="+output), env...))
			return nil
		},
	}, &starts
}

func TestResolveFallsBackToThePreferredHarness(t *testing.T) {
	l, _ := fakeLauncher("claude")
	plan, fallback, err := l.Resolve(testAdapters(), domain.AgentRole{Command: "review"})
	if err != nil {
		t.Fatalf("Resolve() error = %+v", err)
	}
	if plan.Harness != "claude-code" || plan.Model != "haiku" {
		t.Fatalf("plan = %+v, want the preferred harness and model", plan)
	}
	if fallback == nil || fallback.Harness != "claude-code" || !strings.Contains(fallback.Reason, "codex not found") {
		t.Fatalf("fallback = %+v", fallback)
	}

	l, _ = fakeLauncher()
	if _, _, err := l.Resolve(testAdapters(), domain.AgentRole{Command: "review"}); err == nil || err.Code != "harness_missing" {
		t.Fatalf("Resolve() without any installed harness error = %+v, want harness_missing", err)
	}
}

// TestResolveWithoutAChoiceIsUnconfigured verifies a role with no harness
// from the role, preset, or preferences resolves to agent_unconfigured
// naming what is missing.
func TestResolveWithoutAChoiceIsUnconfigured(t *testing.T) {
	l, _ := fakeLauncher("claude")
	adapters := testAdapters()
	adapters.Defaults, adapters.Fallback = domain.AgentDefaults{}, domain.AgentDefaults{}
	_, _, err := l.Resolve(adapters, domain.AgentRole{Key: "security", Command: "review"})
	if err == nil || err.Code != "agent_unconfigured" || !strings.Contains(err.Message, "agents.defaults.harness") {
		t.Fatalf("Resolve() without a harness = %+v, want agent_unconfigured naming agents.defaults.harness", err)
	}
}

// TestResolveTakesTheRoleTimeoutOverTheDefault verifies a role's timeout
// wins over agents.defaults.timeout.
func TestResolveTakesTheRoleTimeoutOverTheDefault(t *testing.T) {
	l, _ := fakeLauncher("codex")
	adapters := testAdapters()
	adapters.Defaults.Timeout = "1h"
	plan, _, err := l.Resolve(adapters, domain.AgentRole{Command: "review", Timeout: "90s"})
	if err != nil || plan.Timeout != 90*time.Second {
		t.Fatalf("Resolve() = %+v, %+v; want the role's 90s", plan, err)
	}
	plan, _, _ = l.Resolve(adapters, domain.AgentRole{Command: "review"})
	if plan.Timeout != time.Hour {
		t.Fatalf("Resolve() timeout = %s, want the default 1h", plan.Timeout)
	}
}

// TestStartDetachesTheSupervisorWithTheRenderedHarness verifies Start runs
// `okt agent run` for the session, detached in the project root with the
// transcript as its output, carrying the rendered harness argv after `--`
// with the prompt inline.
func TestStartDetachesTheSupervisorWithTheRenderedHarness(t *testing.T) {
	t.Setenv("OMAKITEN_HOME", t.TempDir())
	files, err := Files("s-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files.Prompt, []byte("review task #7"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, starts := fakeLauncher("codex")
	plan, _, _ := l.Resolve(testAdapters(), domain.AgentRole{Command: "review", Timeout: "30m"})
	if err := l.Start(context.Background(), plan, Request{Project: "alpha", Cwd: "/repo", Files: files, Env: []string{"OMAKITEN_AGENT_SESSION_ID=s-1"}}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	got := strings.Join((*starts)[0], " ")
	for _, want := range []string{
		" --project alpha agent run --session s-1 --timeout 30m0s -- codex exec --model o4 -- review task #7 ",
		" DIR=/repo ", " OUTPUT=" + files.Transcript + " ", " OMAKITEN_AGENT_SESSION_ID=s-1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("detached start = %q, want %q", got, want)
		}
	}
}

func readTranscript(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// TestSuperviseWritesAJSONLTranscript verifies JSON stdout lines are kept,
// every other line is wrapped with its stream, and the exit code returns.
func TestSuperviseWritesAJSONLTranscript(t *testing.T) {
	transcript := t.TempDir() + "/s.jsonl"
	var started int
	result, err := Supervise(context.Background(), Run{Argv: []string{"sh", "-c", `echo '{"type":"result"}'; echo plain; echo oops >&2; exit 3`}, Transcript: transcript, Started: func(pid int) { started = pid }})
	if err != nil || result.ExitCode != 3 || result.TimedOut || result.Stopped || started == 0 {
		t.Fatalf("Supervise() = %+v, %v (started %d); want exit 3 after reporting the pid", result, err, started)
	}
	lines := readTranscript(t, transcript)
	for _, want := range []string{`{"type":"result"}`, `{"stream":"stdout","text":"plain"}`, `{"stream":"stderr","text":"oops"}`} {
		if !slices.Contains(lines, want) {
			t.Fatalf("transcript = %q, want line %s", lines, want)
		}
	}
}

// TestSuperviseEndsTheHarnessGroup verifies the timeout and ctx's end each
// stop the harness and its children.
func TestSuperviseEndsTheHarnessGroup(t *testing.T) {
	start := time.Now()
	result, err := Supervise(context.Background(), Run{Argv: []string{"sh", "-c", "sleep 30 & wait"}, Transcript: t.TempDir() + "/t.jsonl", Timeout: 100 * time.Millisecond})
	if err != nil || !result.TimedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("Supervise() with a timeout = %+v, %v after %s; want timed out at once", result, err, time.Since(start))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err = Supervise(ctx, Run{Argv: []string{"sh", "-c", "sleep 30 & wait"}, Transcript: t.TempDir() + "/c.jsonl"})
	if err != nil || !result.Stopped {
		t.Fatalf("Supervise() with ctx ended = %+v, %v; want stopped", result, err)
	}
	if _, err := Supervise(context.Background(), Run{Argv: []string{"no-such-harness-binary"}, Transcript: t.TempDir() + "/m.jsonl"}); err == nil {
		t.Fatal("Supervise() of a missing executable succeeded")
	}
}

// TestTheHarnessKeepsTheSessionAlive verifies the harness inherits the
// session lock, so the session lives while the harness does even when its
// supervisor lets go, and Stop ends the harness group.
func TestTheHarnessKeepsTheSessionAlive(t *testing.T) {
	t.Setenv("OMAKITEN_HOME", t.TempDir())
	files, err := Files("s-3")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Hold(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := fakeLauncher()
	pids := make(chan int, 1)
	done := make(chan Result, 1)
	go func() {
		result, _ := Supervise(context.Background(), Run{Argv: []string{"sh", "-c", "sleep 30 & wait"}, Transcript: files.Transcript, Lock: lock, Started: func(pid int) { pids <- pid }})
		done <- result
	}()
	pid := <-pids
	// The supervisor's own descriptor closes, as when it is killed.
	_ = lock.file.Close()
	if !l.Alive("s-3") {
		t.Fatal("the session died with its supervisor's descriptor while the harness runs")
	}
	if err := l.Stop(pid); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() left the harness running")
	}
	if l.Alive("s-3") {
		t.Fatal("the session is alive after its harness group stopped")
	}
}

// TestAliveFollowsTheSupervisorLock verifies a session is alive exactly
// while its supervisor holds the lock.
func TestAliveFollowsTheSupervisorLock(t *testing.T) {
	t.Setenv("OMAKITEN_HOME", t.TempDir())
	files, err := Files("s-2")
	if err != nil {
		t.Fatal(err)
	}
	l, _ := fakeLauncher()
	if l.Alive("s-2") {
		t.Fatal("a session that never locked is alive")
	}
	lock, err := Hold(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Alive("s-2") {
		t.Fatal("a held session is not alive")
	}
	lock.Release()
	if l.Alive("s-2") {
		t.Fatal("a released session is still alive")
	}
}

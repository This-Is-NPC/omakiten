package agentlaunch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

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
	argv := args("tmux", "new-window", "-F", "#{pane_id}", []string{"-t", "{target}"}, "-c", "{cwd}")
	got := Expand(argv, map[string]string{"target": "", "cwd": "/repo"})
	want := []string{"tmux", "new-window", "-F", "#{pane_id}", "-c", "/repo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Expand() = %q, want %q", got, want)
	}
	got = Expand(argv, map[string]string{"target": "work", "cwd": "{target}"})
	if got[4] != "-t" || got[5] != "work" || got[7] != "{target}" {
		t.Fatalf("Expand() = %q, want the group kept and values not re-expanded", got)
	}
}

func TestHarnessCommandReadsPromptInTheShell(t *testing.T) {
	spec := domain.HarnessSpec{Argv: args("claude", []string{"--model", "{model}"}, "{prompt}")}
	got := HarnessCommand(spec, map[string]string{"model": "", "prompt_file": "/tmp/p it.md"}, []string{"OMAKITEN_HOME=/h o", "OMAKITEN_TEAM_MEMBER=lead"})
	script := "export OMAKITEN_HOME='/h o'; export OMAKITEN_TEAM_MEMBER=lead; exec claude \"$(cat '/tmp/p it.md')\""
	if got != "sh -c '"+strings.ReplaceAll(script, "'", `'\''`)+"'" {
		t.Fatalf("HarnessCommand() = %s", got)
	}
}

func fakeLauncher(installed ...string) (Launcher, *[][]string) {
	var calls [][]string
	have := map[string]bool{}
	for _, name := range installed {
		have[name] = true
	}
	return Launcher{
		LookPath: func(name string) (string, error) {
			if have[name] {
				return "/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, argv []string) ([]byte, error) {
			calls = append(calls, argv)
			if argv[0] == "herdr" && argv[1] == "tab" {
				return []byte(`{"result":{"root_pane":{"pane_id":"w1:p3"}}}`), nil
			}
			return []byte("%7\n"), nil
		},
	}, &calls
}

func testAdapters() domain.AgentAdapters {
	return domain.AgentAdapters{
		Defaults:  domain.AgentDefaults{Harness: "codex", Launcher: "tmux"},
		Fallback:  domain.AgentDefaults{Harness: "claude-code", Model: "haiku", Launcher: "herdr"},
		Harnesses: map[string]domain.HarnessSpec{"codex": {Argv: args("codex")}, "claude-code": {Argv: args("claude")}},
		Launchers: map[string]domain.LauncherSpec{
			"tmux":  {Open: args("tmux", "new-window", "{command}"), Send: args("tmux", "send-keys", "-t", "{pane}", "{text}", "Enter")},
			"herdr": {Open: args("herdr", "tab", "create"), Pane: "result.root_pane.pane_id", Run: args("herdr", "pane", "run", "{pane}", "{command}"), Send: args("herdr", "pane", "run", "{pane}", "{text}")},
		},
	}
}

func TestResolveFallsBackToPreferredAdapters(t *testing.T) {
	l, _ := fakeLauncher("claude", "herdr")
	plan, fallback, err := l.Resolve(testAdapters(), domain.AgentRole{Command: "review"})
	if err != nil {
		t.Fatalf("Resolve() error = %+v", err)
	}
	if plan.Harness != "claude-code" || plan.Model != "haiku" || plan.Launcher != "herdr" {
		t.Fatalf("plan = %+v, want the preferred harness, model and launcher", plan)
	}
	if fallback == nil || fallback.Harness != "claude-code" || fallback.Launcher != "herdr" || !strings.Contains(fallback.Reason, "codex not found") {
		t.Fatalf("fallback = %+v", fallback)
	}

	l, _ = fakeLauncher("herdr")
	if _, _, err := l.Resolve(testAdapters(), domain.AgentRole{Command: "review"}); err == nil || err.Code != "harness_missing" {
		t.Fatalf("Resolve() without any installed harness error = %+v, want harness_missing", err)
	}
}

func TestOpenReadsPaneAndRunsTheCommand(t *testing.T) {
	l, calls := fakeLauncher("codex", "tmux", "herdr")
	plan, _, _ := l.Resolve(testAdapters(), domain.AgentRole{Command: "review", Launcher: "herdr"})
	pane, err := l.Open(context.Background(), plan, Request{Cwd: "/repo", PromptFile: "/p.md"})
	if err != nil || pane != "w1:p3" {
		t.Fatalf("Open() = %q, %v; want the JSON pane id", pane, err)
	}
	if run := (*calls)[1]; run[3] != "w1:p3" || !strings.Contains(run[4], "exec codex") {
		t.Fatalf("run step = %q, want the harness started in the opened pane", run)
	}
	if err := l.Send(context.Background(), plan.LauncherSpec, pane, "wake up"); err != nil || (*calls)[2][4] != "wake up" {
		t.Fatalf("Send() = %v, calls %q", err, *calls)
	}
}

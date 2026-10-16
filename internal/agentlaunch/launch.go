// Package agentlaunch opens agent harnesses in terminal panes through the
// harness and launcher adapters of the effective agent configuration.
package agentlaunch

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"omakiten/internal/domain"
	"omakiten/internal/processutil"
)

// openTimeout bounds each launcher command; launchers return once the pane
// exists, without waiting for the harness.
const openTimeout = 10 * time.Second

// submitDelay separates typing a line from submitting it, so the pane sees
// two inputs rather than one paste.
const submitDelay = 150 * time.Millisecond

// Launcher runs adapter commands. LookPath and Run are the process seams;
// SubmitDelay is the pause between a send and its submit.
type Launcher struct {
	LookPath    func(file string) (string, error)
	Run         func(ctx context.Context, argv []string) ([]byte, error)
	SubmitDelay time.Duration
}

// New returns a Launcher that runs real processes.
func New() Launcher {
	return Launcher{LookPath: exec.LookPath, Run: run, SubmitDelay: submitDelay}
}

func run(ctx context.Context, argv []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Plan is a role resolved to concrete adapters.
type Plan struct {
	Harness      string
	HarnessSpec  domain.HarnessSpec
	Model        string
	Launcher     string
	LauncherSpec domain.LauncherSpec
}

// Resolve picks the role's harness and launcher, then the preset's and the
// preferences' defaults; Omakiten chooses none. A seat left without either
// is agent_unconfigured. A selection whose executable is not installed is
// replaced by the user's preferred one and reported; without one it is a
// spawn error. A missing model leaves the harness on its own default.
func (l Launcher) Resolve(adapters domain.AgentAdapters, role domain.AgentRole) (Plan, *domain.AgentFallback, *domain.AgentError) {
	plan := Plan{Harness: cmp.Or(role.Harness, adapters.Defaults.Harness), Model: cmp.Or(role.Model, adapters.Defaults.Model), Launcher: cmp.Or(role.Launcher, adapters.Defaults.Launcher)}
	if perr := unconfigured(role, plan); perr != nil {
		return plan, nil, perr
	}
	fallback, perr := l.resolveHarness(adapters, &plan)
	if perr != nil {
		return plan, nil, perr
	}
	return l.resolveLauncher(adapters, &plan, fallback)
}

// resolveHarness sets the plan's harness spec, replacing a missing
// executable with the user's preferred harness and model.
func (l Launcher) resolveHarness(adapters domain.AgentAdapters, plan *Plan) (*domain.AgentFallback, *domain.AgentError) {
	harness, ok := adapters.Harnesses[plan.Harness]
	if !ok {
		return nil, &domain.AgentError{Code: "harness_missing", Message: fmt.Sprintf("harness %q is not declared", plan.Harness)}
	}
	var fallback *domain.AgentFallback
	if !l.installed(harness.Argv) {
		alt, altOK := adapters.Harnesses[adapters.Fallback.Harness]
		if !altOK || adapters.Fallback.Harness == plan.Harness || !l.installed(alt.Argv) {
			return nil, &domain.AgentError{Code: "harness_missing", Message: fmt.Sprintf("harness %q is not installed and no installed preferred harness replaces it", plan.Harness)}
		}
		fallback = &domain.AgentFallback{Harness: adapters.Fallback.Harness, Reason: fmt.Sprintf("%s not found on PATH", executable(harness.Argv))}
		plan.Harness, harness = adapters.Fallback.Harness, alt
		plan.Model = cmp.Or(adapters.Fallback.Model, plan.Model)
	}
	plan.HarnessSpec = harness
	return fallback, nil
}

// resolveLauncher sets the plan's launcher spec, replacing a missing
// executable with the user's preferred launcher and extending fallback.
func (l Launcher) resolveLauncher(adapters domain.AgentAdapters, plan *Plan, fallback *domain.AgentFallback) (Plan, *domain.AgentFallback, *domain.AgentError) {
	launcher, ok := adapters.Launchers[plan.Launcher]
	if !ok {
		return *plan, fallback, &domain.AgentError{Code: "launcher_missing", Message: fmt.Sprintf("launcher %q is not declared", plan.Launcher)}
	}
	if !l.installed(launcher.Open) {
		alt, altOK := adapters.Launchers[adapters.Fallback.Launcher]
		if !altOK || adapters.Fallback.Launcher == plan.Launcher || !l.installed(alt.Open) {
			return *plan, fallback, &domain.AgentError{Code: "launcher_missing", Message: fmt.Sprintf("launcher %q is not installed and no installed preferred launcher replaces it", plan.Launcher)}
		}
		reason := fmt.Sprintf("%s not found on PATH", executable(launcher.Open))
		if fallback == nil {
			fallback = &domain.AgentFallback{Reason: reason}
		} else {
			fallback.Reason += "; " + reason
		}
		fallback.Launcher = adapters.Fallback.Launcher
		plan.Launcher, launcher = adapters.Fallback.Launcher, alt
	}
	plan.LauncherSpec = launcher
	return *plan, fallback, nil
}

// unconfigured reports the setting a seat lacks and where to set it.
func unconfigured(role domain.AgentRole, plan Plan) *domain.AgentError {
	for _, missing := range []struct{ value, setting string }{{plan.Harness, "harness"}, {plan.Launcher, "launcher"}} {
		if missing.value == "" {
			return &domain.AgentError{Code: "agent_unconfigured", Message: fmt.Sprintf(
				"seat %q names no %s: set %s on the role, agents.defaults.%s in the preset's settings, or agents.defaults.%s in your preferences.yaml (`okt config path --scope global` prints the directory holding it); check the resolved seats with `okt move <task> --to <bucket> --dry-run`",
				role.Key, missing.setting, missing.setting, missing.setting, missing.setting)}
		}
	}
	return nil
}

func (l Launcher) installed(argv []domain.AgentArg) bool {
	name := executable(argv)
	if name == "" {
		return false
	}
	_, err := l.LookPath(name)
	return err == nil
}

func executable(argv []domain.AgentArg) string {
	if len(argv) == 0 || len(argv[0].Values) == 0 {
		return ""
	}
	return argv[0].Values[0]
}

// Request carries what one pane needs: its directory, label, prompt file,
// and the environment the harness inherits.
type Request struct {
	Cwd        string
	Label      string
	PromptFile string
	Vars       map[string]string
	Env        []string
}

// Open starts the planned harness in a new pane and returns the pane id.
func (l Launcher) Open(ctx context.Context, plan Plan, req Request) (string, error) {
	vars := map[string]string{"model": plan.Model, "cwd": req.Cwd, "prompt_file": req.PromptFile}
	for k, v := range req.Vars {
		vars[k] = v
	}
	command := HarnessCommand(plan.HarnessSpec, vars, req.Env)
	launch := map[string]string{"command": command, "cwd": req.Cwd, "label": req.Label, "target": plan.LauncherSpec.Target}
	out, err := l.Run(ctx, Expand(plan.LauncherSpec.Open, launch))
	if err != nil {
		return "", err
	}
	pane, err := paneID(out, plan.LauncherSpec.Pane)
	if err != nil {
		return "", err
	}
	if len(plan.LauncherSpec.Run) > 0 {
		launch["pane"] = pane
		if _, err := l.Run(ctx, Expand(plan.LauncherSpec.Run, launch)); err != nil {
			return pane, err
		}
	}
	return pane, nil
}

// Send types one line into a pane, then presses submit in its own command
// after SubmitDelay.
func (l Launcher) Send(ctx context.Context, spec domain.LauncherSpec, pane, text string) error {
	vars := map[string]string{"pane": pane, "text": text, "target": spec.Target}
	if _, err := l.Run(ctx, Expand(spec.Send, vars)); err != nil || len(spec.Submit) == 0 {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(l.SubmitDelay):
	}
	_, err := l.Run(ctx, Expand(spec.Submit, vars))
	return err
}

func paneID(out []byte, path string) (string, error) {
	if path == "" {
		pane := strings.TrimSpace(string(out))
		if pane == "" {
			return "", fmt.Errorf("launcher printed no pane id")
		}
		return pane, nil
	}
	var value any
	if err := json.Unmarshal(out, &value); err != nil {
		return "", fmt.Errorf("launcher output is not JSON: %w", err)
	}
	for _, key := range strings.Split(path, ".") {
		m, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("launcher output has no %s", path)
		}
		value = m[key]
	}
	switch v := value.(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprint(v), nil
	}
	return "", fmt.Errorf("launcher output has no %s", path)
}

// promptMark stands for the prompt in an expanded harness argv until the
// command line is quoted.
const promptMark = "\x00prompt\x00"

// HarnessCommand returns the shell command that exports env and runs the
// harness. `{prompt}` becomes the prompt file's content, read by that shell.
func HarnessCommand(spec domain.HarnessSpec, vars map[string]string, env []string) string {
	expanded := map[string]string{"prompt": promptMark}
	for k, v := range vars {
		expanded[k] = v
	}
	readPrompt := `"$(cat ` + processutil.ShellQuote(vars["prompt_file"]) + `)"`
	var line []string
	for _, arg := range Expand(spec.Argv, expanded) {
		parts := strings.Split(arg, promptMark)
		for i := range parts {
			if parts[i] != "" || len(parts) == 1 {
				parts[i] = processutil.ShellQuote(parts[i])
			}
		}
		line = append(line, strings.Join(parts, readPrompt))
	}
	var script strings.Builder
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		script.WriteString("export " + name + "=" + processutil.ShellQuote(value) + "; ")
	}
	script.WriteString("exec " + strings.Join(line, " "))
	return "sh -c " + processutil.ShellQuote(script.String())
}

// Expand renders adapter argv. Known placeholders are replaced; unknown
// braces stay literal. A group is dropped when any placeholder in it is empty.
func Expand(args []domain.AgentArg, vars map[string]string) []string {
	var out []string
	for _, arg := range args {
		values := make([]string, 0, len(arg.Values))
		empty := false
		for _, v := range arg.Values {
			rendered, hadEmpty := render(v, vars)
			empty = empty || hadEmpty
			values = append(values, rendered)
		}
		if arg.Group && empty {
			continue
		}
		out = append(out, values...)
	}
	return out
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// render replaces known placeholders in one pass and reports whether any
// of them was empty.
func render(s string, vars map[string]string) (string, bool) {
	empty := false
	out := placeholder.ReplaceAllStringFunc(s, func(token string) string {
		value, ok := vars[token[1:len(token)-1]]
		if !ok {
			return token
		}
		empty = empty || value == ""
		return value
	})
	return out, empty
}

// Package agentlaunch runs agent harnesses headless: it resolves a role to
// a harness command line, starts a detached supervisor process for each
// run, and supervises the harness until it exits.
package agentlaunch

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"

	"omakiten/internal/domain"
)

// Launcher starts supervisors. LookPath and Detach are the process seams:
// Detach starts argv in dir with env, detached from this process, with its
// output appended to the file at output.
type Launcher struct {
	LookPath func(file string) (string, error)
	Detach   func(argv, env []string, dir, output string) error
}

// New returns a Launcher that starts real processes.
func New() Launcher {
	return Launcher{LookPath: exec.LookPath, Detach: detach}
}

// Plan is a role resolved to a concrete harness and run limit.
type Plan struct {
	Harness     string
	HarnessSpec domain.HarnessSpec
	Model       string
	Timeout     time.Duration
}

// Resolve picks the role's harness and timeout, then the preset's and the
// preferences' defaults; Omakiten chooses none. A seat left without a
// harness is agent_unconfigured. A harness whose executable is not
// installed is replaced by the user's preferred one and reported; without
// one it is a spawn error. A missing model leaves the harness on its own
// default.
func (l Launcher) Resolve(adapters domain.AgentAdapters, role domain.AgentRole) (Plan, *domain.AgentFallback, *domain.AgentError) {
	plan := Plan{Harness: cmp.Or(role.Harness, adapters.Defaults.Harness), Model: cmp.Or(role.Model, adapters.Defaults.Model)}
	if plan.Harness == "" {
		return plan, nil, &domain.AgentError{Code: string(domain.ErrAgentUnconfigured), Message: fmt.Sprintf(
			"seat %q names no harness: set harness on the role, agents.defaults.harness in the preset's settings, or agents.defaults.harness in your preferences.yaml (`okt config path --scope global` prints the directory holding it); check the resolved seats with `okt move <task> --to <bucket> --dry-run`",
			role.Key)}
	}
	if timeout := cmp.Or(role.Timeout, adapters.Defaults.Timeout); timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil || d <= 0 {
			return plan, nil, &domain.AgentError{Code: "timeout_invalid", Message: fmt.Sprintf("seat %q timeout %q is not a positive Go duration", role.Key, timeout)}
		}
		plan.Timeout = d
	}
	harness, ok := adapters.Harnesses[plan.Harness]
	if !ok {
		return plan, nil, &domain.AgentError{Code: "harness_missing", Message: fmt.Sprintf("harness %q is not declared", plan.Harness)}
	}
	var fallback *domain.AgentFallback
	if !l.installed(harness.Argv) {
		alt, altOK := adapters.Harnesses[adapters.Fallback.Harness]
		if !altOK || adapters.Fallback.Harness == plan.Harness || !l.installed(alt.Argv) {
			return plan, nil, &domain.AgentError{Code: "harness_missing", Message: fmt.Sprintf("harness %q is not installed and no installed preferred harness replaces it", plan.Harness)}
		}
		fallback = &domain.AgentFallback{Harness: adapters.Fallback.Harness, Reason: fmt.Sprintf("%s not found on PATH", executable(harness.Argv))}
		plan.Harness, harness = adapters.Fallback.Harness, alt
		plan.Model = cmp.Or(adapters.Fallback.Model, plan.Model)
	}
	plan.HarnessSpec = harness
	return plan, fallback, nil
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

// Request carries what one run needs: the project the supervisor reports
// to, its directory, the session's files, the placeholder values of the
// harness argv, and the environment the harness inherits.
type Request struct {
	Project string
	Cwd     string
	Files   SessionFiles
	Vars    map[string]string
	Env     []string
}

// Start starts `okt agent run` detached for the planned harness. The
// harness argv is rendered here, with `{prompt}` replaced by the prompt
// file's content.
func (l Launcher) Start(_ context.Context, plan Plan, req Request) error {
	prompt, err := os.ReadFile(req.Files.Prompt)
	if err != nil {
		return err
	}
	vars := map[string]string{"model": plan.Model, "cwd": req.Cwd, "prompt_file": req.Files.Prompt, "prompt": string(prompt)}
	for k, v := range req.Vars {
		vars[k] = v
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argv := []string{exe, "--project", req.Project, "agent", "run", "--session", req.Files.Session}
	if plan.Timeout > 0 {
		argv = append(argv, "--timeout", plan.Timeout.String())
	}
	argv = append(append(argv, "--"), Expand(plan.HarnessSpec.Argv, vars)...)
	return l.Detach(argv, append(os.Environ(), req.Env...), req.Cwd, req.Files.Transcript)
}

// Alive reports whether any process of a session's run still lives: the
// supervisor and the harness hold the session's lock between them.
func (l Launcher) Alive(sessionID string) bool {
	files, err := Files(sessionID)
	if err != nil {
		return true
	}
	return lockHeld(files.Lock)
}

// Stop asks the harness process group led by pid to stop; its supervisor
// then records the run.
func (l Launcher) Stop(pid int) error {
	return stopGroup(pid)
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

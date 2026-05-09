package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"omakiten/internal/domain"
)

// DefaultExecTimeout is the fallback timeout applied to exec actions
// that omit `timeout_ms`. Chosen to keep blocked scripts from leaking
// goroutines indefinitely while leaving room for genuinely slow tools.
const DefaultExecTimeout = 30 * time.Second

// Exec runs an external command without invoking a shell. Args are
// taken literally from the `argv` array; the entire domain.Event is
// piped to stdin as JSON so scripts can `jq` whatever they need
// without templating in YAML.
//
// Args contract:
//   - argv (required, []string or []any of strings) — argv[0] is the
//     binary, argv[1:] are literal args.
//   - timeout_ms (optional, int) — kill -9 deadline for the child.
//
// Stdout / stderr are captured. Non-zero exit code returns an error
// shaped as "exec %s exited %d: <stderr>". Timeout returns an error
// wrapping context.DeadlineExceeded so engine logs it as failure.
type Exec struct{}

func (Exec) Name() string { return "exec" }

func (e Exec) Execute(ctx context.Context, ev domain.Event, args map[string]any) error {
	argv, err := readArgv(args)
	if err != nil {
		return err
	}
	timeout := readTimeout(args)

	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event for stdin: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// CommandContext + Cancel kills the process on timeout. Force SIGKILL
	// so the engine doesn't wait on a non-cooperative child.
	cmd.Cancel = func() error { return cmd.Process.Kill() }

	if err := cmd.Run(); err != nil {
		stderrTrim := strings.TrimSpace(stderr.String())
		if runCtx.Err() != nil {
			return fmt.Errorf("exec %s timed out after %s: %w", argv[0], timeout, runCtx.Err())
		}
		if stderrTrim != "" {
			return fmt.Errorf("exec %s failed: %w (stderr: %s)", argv[0], err, stderrTrim)
		}
		return fmt.Errorf("exec %s failed: %w", argv[0], err)
	}
	return nil
}

func readArgv(args map[string]any) ([]string, error) {
	raw, ok := args["argv"]
	if !ok {
		return nil, fmt.Errorf("exec: args.argv is required")
	}
	switch v := raw.(type) {
	case []string:
		if len(v) == 0 {
			return nil, fmt.Errorf("exec: args.argv must be non-empty")
		}
		return append([]string(nil), v...), nil
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("exec: args.argv[%d] must be a string, got %T", i, item)
			}
			out = append(out, s)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("exec: args.argv must be non-empty")
		}
		return out, nil
	}
	return nil, fmt.Errorf("exec: args.argv must be an array of strings, got %T", raw)
}

func readTimeout(args map[string]any) time.Duration {
	raw, ok := args["timeout_ms"]
	if !ok {
		return DefaultExecTimeout
	}
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	case float64:
		if v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	}
	return DefaultExecTimeout
}

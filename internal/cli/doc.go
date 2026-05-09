// Package cli is the cobra-rooted command tree behind the `okt` binary.
// Every leaf command parses flags, opens the runtime, calls into
// internal/app or internal/agent, and emits a one-line JSON envelope so
// agents can parse stdout without an interactive terminal. Errors are
// rendered as a coded envelope (ok=false, code, msg, details) so the
// caller can branch on the code without string-matching.
package cli

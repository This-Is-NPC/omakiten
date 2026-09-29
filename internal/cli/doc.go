// Package cli defines the Cobra command tree behind the okt binary.
// Commands validate scoped input and invoke the operation facade through the runtime.
// Data commands emit JSON on stdout; diagnostics, prompts and editors use stderr.
// Command metadata supplies shared validation, help and shell completions.
package cli

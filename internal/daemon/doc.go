// Package daemon composes the local HTTP API from a runtime session: it
// binds the httpapi ports to project runtimes, tails the event log for
// SSE, and owns the single-instance lock, token, and discovery file.
package daemon

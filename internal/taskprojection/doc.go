// Package taskprojection builds the shared task/dependency read model used by
// task-oriented views. It is pure and deterministic: callers provide an
// in-memory snapshot and receive filtered tasks, relations, counts and
// completion classification without I/O or presentation concerns.
package taskprojection

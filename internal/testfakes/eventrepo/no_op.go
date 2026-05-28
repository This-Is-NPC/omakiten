// Package eventrepo provides a no-op fake of app.EventRepository for
// in-tree tests that need to satisfy the interface but do not exercise
// the events surface.
//
// Tests either swap a NoOp wholesale where every method should return
// the zero value, or embed it into a wrapper struct and override only
// the methods that carry assertions. Either way, adding a new method
// to app.EventRepository only requires updating NoOp here — the
// embedders inherit the new no-op default automatically.
//
// This file deliberately avoids importing internal/app: the package is
// consumed by tests inside internal/app itself, and a direct import
// would form a cycle. The compile-time check that NoOp satisfies
// app.EventRepository lives in no_op_test.go (external test package),
// which is the canonical place to pin the contract.
package eventrepo

import (
	"context"
	"time"

	"omakiten/internal/domain"
)

// NoOp is an app.EventRepository implementation whose methods all
// return zero values (nil slices, nil maps, zero structs, nil errors).
// Embed it into a test fake to inherit no-op defaults for every method,
// then override only the methods the test exercises.
type NoOp struct{}

// RecordTaskEvent returns a zero domain.Event with no error.
func (NoOp) RecordTaskEvent(_ context.Context, _, _ int64, _, _, _ string) (domain.Event, error) {
	return domain.Event{}, nil
}

// RecordEntityEvent returns no error.
func (NoOp) RecordEntityEvent(_ context.Context, _ string, _, _ int64, _, _ string) error {
	return nil
}

// ListTaskActivity returns a nil slice with no error.
func (NoOp) ListTaskActivity(_ context.Context, _, _ int64, _ string) ([]domain.Event, error) {
	return nil, nil
}

// ListEvents returns a nil slice with no error.
func (NoOp) ListEvents(_ context.Context, _ domain.EventFilter) ([]domain.EventRow, error) {
	return nil, nil
}

// EventCategoryCounts returns a nil map with no error.
func (NoOp) EventCategoryCounts(_ context.Context, _ int64, _ time.Time) (map[domain.EventCategory]int, error) {
	return nil, nil
}

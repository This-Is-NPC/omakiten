package eventrepo_test

import (
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/testfakes/eventrepo"
)

// TestNoOpImplementsEventRepository documents intent: the NoOp fake
// must satisfy app.EventRepository so test packages can embed it
// without manually implementing the full method set. The compile-time
// assertion inside the eventrepo package already guarantees this, but
// the test form makes the contract visible to anyone scanning *_test.go.
func TestNoOpImplementsEventRepository(t *testing.T) {
	var _ app.EventRepository = (*eventrepo.NoOp)(nil)
	var _ app.EventRepository = eventrepo.NoOp{}
}

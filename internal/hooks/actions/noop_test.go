package actions

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
)

func TestNoopHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (Noop{}).Execute(ctx, domain.Event{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context canceled", err)
	}
}

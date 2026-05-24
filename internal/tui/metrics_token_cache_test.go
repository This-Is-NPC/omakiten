package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// countingTokenCounter wraps a real counter so the test can assert how
// often Count actually fires on the underlying tokenizer. Cache hits
// must not advance the counter.
type countingTokenCounter struct {
	calls int
}

func (c *countingTokenCounter) Count(text string) int {
	c.calls++
	return len(text)
}

// TestComputeMetricsTokenCacheReusesPriorCounts pins the contract: a
// second computeMetrics call with the same bodies makes zero new
// Count calls; appending a new comment counts only the new body.
func TestComputeMetricsTokenCacheReusesPriorCounts(t *testing.T) {
	model := buildRefreshHotPathModel(t)
	counter := &countingTokenCounter{}
	model.counter = counter
	model.laws = []domain.Law{{Key: "law-1", Body: "stay in scope"}}
	model.personas = []domain.Persona{{Key: "p", Description: "describes a backend agent"}}
	model.comments = []domain.Comment{{Body: "first comment"}, {Body: "second comment"}}

	_ = model.computeMetrics(0)
	first := counter.calls
	if first == 0 {
		t.Fatalf("first computeMetrics did not call counter")
	}

	_ = model.computeMetrics(0)
	if counter.calls != first {
		t.Fatalf("second computeMetrics fired %d new Count calls; cache did not short-circuit", counter.calls-first)
	}

	model.comments = append(model.comments, domain.Comment{Body: "third comment"})
	_ = model.computeMetrics(0)
	if got := counter.calls - first; got != 1 {
		t.Fatalf("after appending one new comment, expected 1 fresh Count call, got %d", got)
	}
}

// TestCountTokensNilCacheDegradesToDirectCount asserts the value-
// receiver fallback: if tokenCountCache is nil (uninitialised test
// fixture), countTokens still returns the counter's answer instead
// of panicking.
func TestCountTokensNilCacheDegradesToDirectCount(t *testing.T) {
	counter := &countingTokenCounter{}
	m := Model{counter: counter}
	if got := m.countTokens("hi"); got != 2 {
		t.Fatalf("nil-cache fallback returned %d, want 2", got)
	}
	if counter.calls != 1 {
		t.Fatalf("nil-cache should still call counter once; got %d", counter.calls)
	}
}

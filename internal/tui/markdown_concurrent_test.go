package tui

import (
	"sync"
	"testing"

	"omakiten/internal/config"
)

// TestMarkdownRendererConcurrentRenderMatchesSequential pins the W3
// #246 fix: glamour's *TermRenderer is now reused across goroutines
// (per-width cache in r.renderers), so the Render() miss path must
// hold r.mu across tr.Render(body) — otherwise two callers can race
// the shared TermRenderer's internal state and produce corrupted
// output. The test races N goroutines through Render at the same
// width with mixed bodies, then compares the multiset of outputs
// against the sequential baseline. Any mismatch (panic, truncated
// frame, mixed-body interleave) trips the test.
func TestMarkdownRendererConcurrentRenderMatchesSequential(t *testing.T) {
	bodies := []string{
		"# Heading one\n\nFirst paragraph with **bold** text.",
		"# Heading two\n\nSecond paragraph with *italic* text.",
		"## Section\n\n- bullet one\n- bullet two\n- bullet three",
		"Plain prose body without any decoration whatsoever.",
		"> Block quote with `inline code` inside.",
	}
	const width = 60
	const goroutines = 32

	theme := config.Theme{Key: "test", Colors: map[string]string{}}
	tokens := tokensFromTheme(theme)

	// Sequential baseline — each body rendered once in a fresh
	// renderer to lock in the expected output bytes.
	baseline := make(map[string]string, len(bodies))
	for _, body := range bodies {
		r := newMarkdownRenderer(tokens)
		baseline[body] = r.Render(body, width)
	}

	// Concurrent victim renderer — shared by all goroutines.
	r := newMarkdownRenderer(tokens)
	var wg sync.WaitGroup
	results := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		body := bodies[i%len(bodies)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.Render(body, width)
		}()
	}
	wg.Wait()

	for i, got := range results {
		body := bodies[i%len(bodies)]
		want := baseline[body]
		if got != want {
			t.Fatalf("goroutine %d body %q: concurrent render mismatch\nwant:\n%q\n got:\n%q", i, body[:min(24, len(body))], want, got)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

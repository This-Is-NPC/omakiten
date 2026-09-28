package studio

import (
	"fmt"
	"strings"
	"testing"

	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// One keystroke inside each of the three scrollable inspector zones, measured on
// the path the host drives: Update, then View.
//
// The report was "quando eu navego no preview do markdown o app trava" and "ao
// navegar nas tabelas de history do hooks e do related o desempenho fica um
// lixo", and the three shapes fail differently:
//
//   - PREVIEW is a DOCUMENT. Its zone composes every line of a resolved prompt
//     into a bordered row on every render, and the arranger windows the result —
//     so the cost was the whole document however few rows were on screen. At
//     fifteen hundred lines one keystroke cost forty-one milliseconds, which is
//     slower than a key repeat and reads as a freeze.
//   - HISTORY and RELATED are TABLES of a few dozen rows, so composition was
//     never their problem. Theirs was the bundle: every body asked the draft for
//     the candidate through a report that deep-clones both bundles and diffs
//     them, and a keystroke renders every body two or three times.
//
// Keep them. A regression here is invisible in a golden and unmistakable in a
// hand.
func BenchmarkStudioInspectorScroll(b *testing.B) {
	b.Run("preview", func(b *testing.B) {
		screen, frame := commandsPreviewScreen(b, 200, 60, benchLongPreview(1500))
		entered := screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
		entered = benchDrive(b, entered, frame, "tab", "tab")
		if entered.grid.Focus() != sectionCommandsPreview {
			b.Fatalf("focus = %q, want the preview zone", entered.grid.Focus())
		}
		benchScroll(b, entered, frame)
	})
	b.Run("preview-stacked", func(b *testing.B) {
		screen, frame := commandsPreviewScreen(b, 80, 55, benchLongPreview(3000))
		entered := screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
		entered = benchDrive(b, entered, frame, "tab", "tab")
		if entered.grid.Focus() != sectionCommandsPreview {
			b.Fatalf("focus = %q, want the preview zone", entered.grid.Focus())
		}
		benchScroll(b, entered, frame)
	})
	b.Run("hooks-history", func(b *testing.B) {
		deps, err := StudioHooksDeps(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		frame := screentest.FrameAt(b, 200, 60)
		entered := New().Bind(screenhost.StudioHooks, deps).
			Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
		entered = benchDrive(b, entered, frame, "tab", "tab")
		benchScroll(b, entered, frame)
	})
	b.Run("personas-related", func(b *testing.B) {
		deps, err := StudioPersonasDeps(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		frame := screentest.FrameAt(b, 200, 60)
		entered := New().Bind(screenhost.StudioPersonas, deps).
			Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
		entered = benchDrive(b, entered, frame, "tab", "tab")
		benchScroll(b, entered, frame)
	})
}

func benchScroll(b *testing.B, screen Screen, frame screenhost.Frame) {
	b.Helper()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		screen = benchDrive(b, screen, frame, "j")
		studioBenchSink = screen.View(frame)
	}
}

func benchDrive(tb testing.TB, screen Screen, frame screenhost.Frame, keys ...string) Screen {
	tb.Helper()
	for _, key := range keys {
		out := screen.Update(frame, screentest.Key(key))
		next, ok := out.Screen.(Screen)
		if !ok {
			tb.Fatalf("Update(%q) carried %T", key, out.Screen)
		}
		screen = studioPump(tb, next, frame, out.Command)
	}
	return screen
}

// benchLongPreview is a prompt of the shape a real persona-plus-skills
// resolution produces: prose lines wide enough that the arranger has to wrap
// them at any inspector width worth measuring.
func benchLongPreview(lines int) string {
	out := make([]string, lines)
	for i := range out {
		out[i] = fmt.Sprintf("- **item-%04d** — a sentence of prose long enough that the arranger has to wrap it at an inspector width, number %d.", i, i)
	}
	return strings.Join(out, "\n")
}

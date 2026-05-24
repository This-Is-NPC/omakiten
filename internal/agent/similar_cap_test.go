package agent

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestSimilarTasksCapsLongDescriptionBudget pins the W8 #236 cap:
// a task carrying a giant description still surfaces when the
// query matches words inside the first similarTasksTextCapBytes
// bytes, and the loop does not blow into the hundreds of
// microseconds tokenising the trailing 99 KB of body text. The
// regression is structural rather than timing-based: build a
// task whose first 1 KB contains the query and whose tail does
// not, and assert it scores positive.
func TestSimilarTasksCapsLongDescriptionBudget(t *testing.T) {
	queryWord := "wibble"
	tail := strings.Repeat("alphabet ", 16*1024) // ~144 KB of noise, no query word
	task := domain.Task{
		ID:          7,
		Title:       "Track " + queryWord,
		Description: "first paragraph mentions wibble explicitly. " + tail,
	}
	got := similarTasks(queryWord, []domain.Task{task}, 5, nil, nil)
	if len(got) != 1 {
		t.Fatalf("similarTasks returned %d hits, want 1 (cap should still let the head match)", len(got))
	}
	if got[0].ID != 7 {
		t.Fatalf("returned task id = %d, want 7", got[0].ID)
	}
}

// TestSimilarTasksIgnoresMatchPastCap proves the cap is enforced:
// a task whose ONLY occurrence of the query word lives past the
// similarTasksTextCapBytes boundary is invisible to the loop.
// Without the cap the trailing word would still hit; with it the
// task drops out.
func TestSimilarTasksIgnoresMatchPastCap(t *testing.T) {
	queryWord := "wibble"
	prefix := strings.Repeat("a ", similarTasksTextCapBytes) // > cap of unrelated bytes
	task := domain.Task{
		ID:          8,
		Title:       "Title without the keyword",
		Description: prefix + " " + queryWord,
	}
	if got := similarTasks(queryWord, []domain.Task{task}, 5, nil, nil); len(got) != 0 {
		t.Fatalf("similarTasks returned %d hits, want 0 (cap should hide the trailing word)", len(got))
	}
}

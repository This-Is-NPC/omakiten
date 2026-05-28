package tui

import (
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
)

// TestRenderSettingsGuards_AllFourCellStates locks the contract that
// renderSettingsGuards surfaces every cell state declared on the
// active workflow's transition matrix. The omakase fixture mounts a
// 3-bucket workflow (backlog / dev / done) with:
//
//   - backlog → dev    : allowed with a single `comments_tagged` guard
//   - dev     → done   : allowed with zero guards
//   - backlog → done   : NOT declared (no entry in `transitions`)
//   - backlog → backlog: diagonal
//
// The render must emit:
//
//   - the guard slug `comments_tagged` for the with-guards cell
//   - the `[empty]` sentinel for the allowed-with-zero-guards cell
//   - the `—` sentinel for both the diagonal and the disallowed cell
//
// Covers DoD checkbox 3 (render test green for all four cell states).
func TestRenderSettingsGuards_AllFourCellStates(t *testing.T) {
	bundle := config.Bundle{
		Kit:    config.Kit{Key: "omakase"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "omakase"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "omakase",
			Name: "Omakase",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
				{ID: 3, Key: "done", Name: "Done", Position: 3},
			},
			Transitions: []config.Transition{
				{From: 1, To: 2, Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: "self-branch", Count: 1}}},
				{From: 2, To: 3},
				// backlog→done intentionally omitted to exercise the
				// "disallowed (no transition entry)" cell state.
			},
		}},
	}
	snap := config.BuildSnapshot(bundle)

	m := Model{
		styles:    newStyles(config.Theme{}),
		width:     160,
		height:    40,
		top:       topSettings,
		workflow:  snap.Workflow(),
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
		repos:     Repositories{Cache: runtimecache.Install(0, snap)},
	}

	body := m.renderSettingsGuardsBody()

	// State 1: allowed-with-guards → guard slug renders verbatim.
	if !strings.Contains(body, "comments_tagged") {
		t.Fatalf("expected guard slug `comments_tagged` on backlog→dev cell; body:\n%s", body)
	}

	// State 2: allowed-with-zero-guards → `[empty]` sentinel.
	if !strings.Contains(body, "[empty]") {
		t.Fatalf("expected `[empty]` sentinel for dev→done cell; body:\n%s", body)
	}

	// State 3 + 4: diagonal AND disallowed both render the `—`
	// sentinel; assert the body contains at least two occurrences so
	// neither state is silently collapsed to the other.
	dash := "—"
	if got := strings.Count(body, dash); got < 2 {
		t.Fatalf("expected `%s` sentinel in at least two cells (diagonal + disallowed); got %d in body:\n%s", dash, got, body)
	}

	// Header labels must surface so the matrix reads as a from/to
	// table — not just a grid of glyphs.
	if !strings.Contains(body, "FROM") || !strings.Contains(body, "TO") {
		t.Fatalf("expected `from \\ to` header in body; got:\n%s", body)
	}
}

// TestRenderSettingsGuards_HintRenders pins the footer hint surface
// so the read-only message stays reachable from every Settings sub.
// The hint comes from `tui.settings.guards_hint` — wired by Wave 2
// across all 21 locale packs.
func TestRenderSettingsGuards_HintRenders(t *testing.T) {
	m := Model{
		styles:   newStyles(config.Theme{}),
		width:    160,
		height:   40,
		top:      topSettings,
		workflow: domain.Workflow{Key: "omakase", Buckets: []domain.Bucket{{ID: 1, Key: "backlog", Position: 1}, {ID: 2, Key: "dev", Position: 2}}},
	}

	out := m.renderSettingsGuards()
	if !strings.Contains(out, "$EDITOR omakiten.yaml") {
		t.Fatalf("expected read-only hint to render; got:\n%s", out)
	}
}

// TestRenderSettingsGuards_NilSnapshotDegrades guards the renderer's
// behavior when `m.repos.Cache` is unwired (common in light test
// fixtures that bypass the runtime composition root). Allowed
// transitions must still render — the guard payload silently degrades
// to `[empty]` because the transition list alone cannot surface guard
// slugs.
func TestRenderSettingsGuards_NilSnapshotDegrades(t *testing.T) {
	m := Model{
		styles: newStyles(config.Theme{}),
		width:  160,
		height: 40,
		top:    topSettings,
		workflow: domain.Workflow{
			Key:     "omakase",
			Buckets: []domain.Bucket{{ID: 1, Key: "backlog", Position: 1}, {ID: 2, Key: "dev", Position: 2}},
			Transitions: []domain.WorkflowTransition{
				{FromBucketID: 1, FromBucketKey: "backlog", ToBucketID: 2, ToBucketKey: "dev"},
			},
		},
	}

	body := m.renderSettingsGuardsBody()
	if !strings.Contains(body, "[empty]") {
		t.Fatalf("expected allowed cell to render `[empty]` sentinel when snapshot is nil; body:\n%s", body)
	}
	if !strings.Contains(body, "—") {
		t.Fatalf("expected disallowed/diagonal cell to render `—` sentinel; body:\n%s", body)
	}
}

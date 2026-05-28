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

// guardsModelFromBundle is a small fixture builder used by the
// subtask-kit tests below. Inflates `bundle` into a Snapshot, installs
// it in a runtimecache, and wires the Model fields the renderer reads.
// Optionally takes a child snapshot to weld onto `snap.subtaskKitSnapshot`
// via the loader-level SubtaskBundle field so `SubtaskKit()` returns it.
func guardsModelFromBundle(t *testing.T, root config.Bundle, sub *config.Bundle) Model {
	t.Helper()
	if sub != nil {
		root.SubtaskBundle = sub
	}
	snap := config.BuildSnapshot(root)
	return Model{
		styles:    newStyles(config.Theme{}),
		width:     200,
		height:    60,
		top:       topSettings,
		workflow:  snap.Workflow(),
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
		repos:     Repositories{Cache: runtimecache.Install(0, snap)},
	}
}

// guardsOmakaseBundle returns a minimal 3-bucket fixture with the
// supplied per-transition guard list. Used by all three subtask-kit
// cases below so root/sub fixtures only differ on guard payload.
func guardsOmakaseBundle(extraGuards []config.TransitionGuard) config.Bundle {
	return config.Bundle{
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
				{From: 1, To: 2, Guards: extraGuards},
				{From: 2, To: 3},
			},
		}},
	}
}

// TestRenderSettingsGuards_NoSubtaskKit_SingleMatrix locks the
// no-subtask-kit path: when the root snapshot does not configure
// `subtask_kit`, the renderer must collapse to a single matrix using
// the existing `tui.settings.guards_tab` kicker. The two new dual-mode
// kickers must NOT appear so the visual surface for projects without
// a sub-kit stays byte-identical to the pre-cascade renderer.
func TestRenderSettingsGuards_NoSubtaskKit_SingleMatrix(t *testing.T) {
	m := guardsModelFromBundle(t, guardsOmakaseBundle(nil), nil)

	body := m.renderSettingsGuardsBody()

	// Single-matrix path keeps the original guards-tab kicker.
	if !strings.Contains(body, "GUARDS") {
		t.Fatalf("expected `GUARDS` kicker on single-matrix render; body:\n%s", body)
	}
	// Dual-mode kickers must NOT leak into the single-matrix path.
	if strings.Contains(body, "ROOT") {
		t.Fatalf("did not expect ROOT kicker when no subtask_kit configured; body:\n%s", body)
	}
	if strings.Contains(body, "SUBTASK KIT") {
		t.Fatalf("did not expect SUBTASK KIT kicker when no subtask_kit configured; body:\n%s", body)
	}
}

// TestRenderSettingsGuards_SubtaskKitIdentical_SingleMatrix locks the
// equality short-circuit: when the root snapshot configures a
// `subtask_kit` but the resolved sub-kit matches the root on bucket
// ordering, transition set, and per-cell guard payload, the renderer
// must collapse to a single matrix (no `ROOT` / `SUBTASK KIT` kickers).
// This is the common case for projects that wire a sub-kit symbolically
// while keeping its guards aligned with the root.
func TestRenderSettingsGuards_SubtaskKitIdentical_SingleMatrix(t *testing.T) {
	guards := []config.TransitionGuard{{Type: "comments_tagged", Tag: "self-branch", Count: 1}}
	root := guardsOmakaseBundle(guards)
	sub := guardsOmakaseBundle(guards)
	m := guardsModelFromBundle(t, root, &sub)

	body := m.renderSettingsGuardsBody()

	if !strings.Contains(body, "GUARDS") {
		t.Fatalf("expected `GUARDS` kicker when root/sub are guard-identical; body:\n%s", body)
	}
	if strings.Contains(body, "ROOT") || strings.Contains(body, "SUBTASK KIT") {
		t.Fatalf("did not expect dual kickers when root/sub are guard-identical; body:\n%s", body)
	}
	// Only one matrix worth of guard slugs renders.
	if got := strings.Count(body, "comments_tagged"); got != 1 {
		t.Fatalf("expected guard slug `comments_tagged` exactly once on single-matrix render; got %d; body:\n%s", got, body)
	}
}

// TestRenderSettingsGuards_SubtaskKitDiverges_DualMatrix locks the
// dual-matrix path: when root and sub-kit declare different guard
// payloads on at least one cell, the renderer must stack TWO matrices
// labelled with `ROOT` / `SUBTASK KIT` kickers. Both guard slugs (one
// per kit) must render — neither side may be silently dropped.
func TestRenderSettingsGuards_SubtaskKitDiverges_DualMatrix(t *testing.T) {
	root := guardsOmakaseBundle([]config.TransitionGuard{{Type: "comments_tagged", Tag: "self-branch", Count: 1}})
	sub := guardsOmakaseBundle([]config.TransitionGuard{{Type: "comments_min", Count: 3}})
	m := guardsModelFromBundle(t, root, &sub)

	body := m.renderSettingsGuardsBody()

	// Dual mode swaps the `GUARDS` kicker for two specific kickers.
	if !strings.Contains(body, "ROOT") {
		t.Fatalf("expected `ROOT` kicker on dual-matrix render; body:\n%s", body)
	}
	if !strings.Contains(body, "SUBTASK KIT") {
		t.Fatalf("expected `SUBTASK KIT` kicker on dual-matrix render; body:\n%s", body)
	}
	// Both kits surface their guard payloads — one slug per matrix.
	if !strings.Contains(body, "comments_tagged") {
		t.Fatalf("expected root guard slug `comments_tagged` on dual-matrix render; body:\n%s", body)
	}
	if !strings.Contains(body, "comments_min") {
		t.Fatalf("expected sub-kit guard slug `comments_min` on dual-matrix render; body:\n%s", body)
	}
}

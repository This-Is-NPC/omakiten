package app

import (
	"strings"
	"testing"

	"omakiten/defaults"
	"omakiten/internal/config"
)

// loadEmbeddedTemplate reads a default note template from the embedded
// defaults FS so this test reflects what the binary actually ships.
// Cross-checks the auto-discovery contract (#359 W2 AC #6): the renderer
// must see the same bytes the loader would.
func loadEmbeddedTemplate(t *testing.T, slug string) string {
	t.Helper()
	raw, err := defaults.FS.ReadFile("templates/" + slug + ".md")
	if err != nil {
		t.Fatalf("read %s: %v", slug, err)
	}
	_, body, err := config.SplitFrontmatter(raw)
	if err != nil {
		t.Fatalf("split %s: %v", slug, err)
	}
	return string(body)
}

func TestRenderNoteHandoffAllSlotsFilled(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-handoff")
	out, err := RenderNoteTemplate("note-handoff", body, map[string]any{
		"ProjectName":        "Omakiten",
		"ProjectSlug":        "omakiten",
		"ProjectRoot":        "/home/howl/Projects/person/omakiten",
		"WindowSince":        "2026-05-20",
		"SinceCounts":        "12 tasks moved, 4 notes",
		"PreviousHandoffRef": "note #88",
		"ActiveTasks":        "- #361 templates\n- #362 skills",
		"ActivePlanWave":     "wave 2",
		"RecentProgress":     "- N1 storage shipped",
		"DecisionsWindow":    "- locked v1 slot bag",
		"DiscardsWindow":     "- discarded per-preset variants",
		"DepsUnmet":          "- #360 → #361",
		"BlockerComments":    "- #361 awaits renderer",
		"NextSteps":          "- claim #362",
		"ExtraNote":          "wave 3 starts after both green",
		"AuthorModel":        "claude-opus-4-7",
		"Timestamp":          "2026-05-28T23:55:00Z",
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireContains(t, out,
		"# Handoff — Omakiten",
		"## Since last handoff",
		"## Active work",
		"## Recent progress",
		"## Decisions and discards",
		"## Blockers",
		"## Next steps",
		"## Extra context",
		"claude-opus-4-7",
	)
}

func TestRenderNoteHandoffCollapsesEmptySections(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-handoff")
	out, err := RenderNoteTemplate("note-handoff", body, map[string]any{
		"ProjectName":    "Omakiten",
		"ProjectSlug":    "omakiten",
		"ActiveTasks":    "- #361 templates",
		"RecentProgress": "- N1 storage shipped",
		"AuthorModel":    "claude-opus-4-7",
		"Timestamp":      "2026-05-28T23:55:00Z",
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireContains(t, out, "## Active work", "## Recent progress")
	requireAbsent(t, out,
		"## Since last handoff",
		"## Decisions and discards",
		"## Blockers",
		"## Next steps",
		"## Extra context",
	)
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("collapse failed; triple newline present:\n%s", out)
	}
}

func TestRenderNoteRecapAllSlotsFilled(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-recap")
	out, err := RenderNoteTemplate("note-recap", body, map[string]any{
		"WindowSince":     "2026-05-20",
		"WindowUntil":     "2026-05-28",
		"TasksDoneWindow": "- #360 storage",
		"AuthorModel":     "claude-opus-4-7",
		"Timestamp":       "2026-05-28T23:55:00Z",
		"NotesByKind": []map[string]string{
			{"Kind": "handoff", "Body": "- handoff #88 — wave 1 close"},
			{"Kind": "decision", "Body": "- locked v1 slot bag"},
		},
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireContains(t, out,
		"# Recap — 2026-05-20 → 2026-05-28",
		"## Notes by kind",
		"### handoff",
		"### decision",
		"## Tasks moved to done",
	)
}

func TestRenderNoteRecapCollapsesEmptySections(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-recap")
	out, err := RenderNoteTemplate("note-recap", body, map[string]any{
		"WindowSince": "2026-05-20",
		"WindowUntil": "2026-05-28",
		"AuthorModel": "claude-opus-4-7",
		"Timestamp":   "2026-05-28T23:55:00Z",
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireAbsent(t, out, "## Notes by kind", "## Tasks moved to done")
}

func TestRenderNoteStandupDigestPerProjectLoop(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-standup-digest")
	out, err := RenderNoteTemplate("note-standup-digest", body, map[string]any{
		"Window":       "2026-05-27 → 2026-05-28",
		"ProjectCount": 2,
		"Projects": []map[string]string{
			{
				"Name":              "Omakiten",
				"LatestHandoff":     "- wave 2 in progress",
				"DeltaSinceHandoff": "4 new tasks",
			},
			{
				"Name":              "Omashiki",
				"LatestHandoff":     "- panel refactor merged",
				"DeltaSinceHandoff": "1 new note",
			},
		},
		"AuthorModel": "claude-opus-4-7",
		"Timestamp":   "2026-05-28T23:55:00Z",
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireContains(t, out,
		"# Standup digest",
		"## Omakiten",
		"## Omashiki",
		"wave 2 in progress",
		"panel refactor merged",
		"**Projects** — 2",
	)
}

func TestRenderNoteStandupDigestCollapsesEmptyProjectBlocks(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-standup-digest")
	out, err := RenderNoteTemplate("note-standup-digest", body, map[string]any{
		"Window":       "2026-05-27 → 2026-05-28",
		"ProjectCount": 1,
		"Projects": []map[string]string{
			{"Name": "Omakiten"},
		},
		"AuthorModel": "claude-opus-4-7",
		"Timestamp":   "2026-05-28T23:55:00Z",
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireContains(t, out, "## Omakiten")
	requireAbsent(t, out, "**Latest handoff**", "**Delta since handoff**")
}

func TestRenderNoteFreeAllSlotsFilled(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-free")
	out, err := RenderNoteTemplate("note-free", body, map[string]any{
		"Title":       "Quick gotcha",
		"Body":        "Workflows reload on bundle save.",
		"Kind":        "gotcha",
		"Timestamp":   "2026-05-28T23:55:00Z",
		"AuthorModel": "claude-opus-4-7",
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireContains(t, out,
		"# Quick gotcha",
		"Workflows reload on bundle save.",
		"_kind: gotcha_",
		"_author: claude-opus-4-7_",
	)
}

func TestRenderNoteFreeCollapsesEmptyFooterParts(t *testing.T) {
	body := loadEmbeddedTemplate(t, "note-free")
	out, err := RenderNoteTemplate("note-free", body, map[string]any{
		"Title": "Free note",
		"Body":  "Lone body.",
	})
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	requireContains(t, out, "# Free note", "Lone body.")
	requireAbsent(t, out, "_kind:", "_author:")
}

func TestRenderNoteTemplateEmptyBodyReturnsEmpty(t *testing.T) {
	out, err := RenderNoteTemplate("note-handoff", "", nil)
	if err != nil {
		t.Fatalf("RenderNoteTemplate: %v", err)
	}
	if out != "" {
		t.Fatalf("empty body → %q, want empty string", out)
	}
}

func TestRenderNoteTemplateInvalidSyntaxReturnsError(t *testing.T) {
	_, err := RenderNoteTemplate("broken", "{{if .X}}", nil)
	if err == nil {
		t.Fatal("RenderNoteTemplate: error = nil, want parse failure")
	}
}

func requireContains(t *testing.T, out string, fragments ...string) {
	t.Helper()
	for _, frag := range fragments {
		if !strings.Contains(out, frag) {
			t.Fatalf("output missing %q\n--- output ---\n%s", frag, out)
		}
	}
}

func requireAbsent(t *testing.T, out string, fragments ...string) {
	t.Helper()
	for _, frag := range fragments {
		if strings.Contains(out, frag) {
			t.Fatalf("output unexpectedly contains %q\n--- output ---\n%s", frag, out)
		}
	}
}

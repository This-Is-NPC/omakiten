package studio

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func personasScreen(tb testing.TB, width, height int, state State) (Screen, screenhost.Frame) {
	tb.Helper()
	deps, err := StudioPersonasDeps(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	frame := screentest.FrameAt(tb, width, height)
	screen := New().Bind(screenhost.StudioPersonas, deps).WithState(state)
	return screen.withFrame(frame), frame
}

func TestStudioPersonasColsStacksAt80AndSitsBesideAt120(t *testing.T) {
	t.Parallel()

	cases := []struct {
		width, height int
		want          screenlayout.Arrangement
	}{
		{80, 24, screenlayout.Stacked},
		{96, 24, screenlayout.SideBySide},
		{120, 40, screenlayout.SideBySide},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			t.Parallel()
			screen, frame := personasScreen(t, tc.width, tc.height, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			res := screen.withFrame(frame).personasGridResult()
			assertStudioColsBreakpoint(t, res, sectionPersonas, sectionPersonasList, personasZones, tc.width, tc.height, tc.want)
			if tc.want != screenlayout.Stacked {
				return
			}
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioListIsOffScreen(t, screen.withFrame(frame).personasGridResult(), sectionPersonasList, sectionPersonasFields, tc.width, tc.height)
			screen = studioDrive(t, screen, frame, "tab")
			screen = studioDrive(t, screen, frame, "tab")
			assertStudioPaintedLeaf(t, screen.withFrame(frame).personasGridResult(), sectionPersonasList, tc.width, tc.height)
		})
	}
}

func TestStudioPersonasTabWalksListThenInspector(t *testing.T) {
	t.Parallel()

	screen, frame := personasScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := screen.grid.Focus(); got != sectionPersonasList {
		t.Fatalf("entry focus = %q, want %q", got, sectionPersonasList)
	}
	for i, want := range []screenlayout.ID{sectionPersonasFields, sectionPersonasRelated, sectionPersonasList} {
		screen = studioDrive(t, screen, frame, "tab")
		if got := screen.grid.Focus(); got != want {
			t.Fatalf("after tab %d focus = %q, want %q", i+1, got, want)
		}
	}
}

func TestStudioPersonasCtrlSOpensApplyFromCols(t *testing.T) {
	t.Parallel()

	editor := &applyMemoryEditor{path: "omakiten.yaml", bundle: applyTestBundle()}
	screen, frame := applyTestScreen(t, screenhost.StudioPersonas, editor)
	if err := screen.studioDraft.RenameBucket(1, "Inbox").ValidationError; err != nil {
		t.Fatalf("RenameBucket: %v", err)
	}
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionPersonasFields {
		t.Fatalf("tab did not land on inspector: %q", screen.grid.Focus())
	}
	outcome := screen.Update(frame, screentest.Key("ctrl+s"))
	next, ok := outcome.Screen.(Screen)
	if !ok {
		t.Fatalf("Update carried %T", outcome.Screen)
	}
	if outcome.Action.Kind == screenhost.ActionNavigate {
		t.Fatalf("inspector ctrl+s navigated to %s; want overlay Stay", outcome.Action.Target)
	}
	if !next.ApplyOverlayOpen() {
		t.Fatal("inspector ctrl+s did not open the apply overlay")
	}
}

func TestStudioPersonasRosterIsNarutoSeven(t *testing.T) {
	t.Parallel()

	rows := studioPersonaRows(studioPersonasGoldenBundle(), nil)
	if len(rows) != 7 {
		t.Fatalf("roster len = %d, want 7", len(rows))
	}
	got := make([]string, 0, len(rows))
	counts := map[string]int{}
	for _, row := range rows {
		got = append(got, row.Persona.Slug)
		counts[row.Persona.Slug] = len(row.Commands)
	}
	want := []string{
		"shikamaru-nara",
		"third-hokage",
		"konohamaru-class",
		"naruto-uzumaki",
		"sakura-haruno",
		"kakashi-hatake",
		"iruka-umino",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roster = %v, want %v", got, want)
	}
	for slug, count := range counts {
		if count != 0 {
			t.Fatalf("%s commands = %d, want no embedded commands", slug, count)
		}
	}
}

func TestStudioPersonasListMatchesNarutoShape(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}} {
		size := size
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			t.Parallel()
			screen, frame := personasScreen(t, size.width, size.height, State{})
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			assertPersonasListShape(t, screen.View(frame), size.width, size.height)
		})
	}
}

func assertPersonasListShape(t *testing.T, rawView string, width, height int) {
	t.Helper()
	view := screentest.StripANSI(rawView)
	for _, want := range []string{"PERSONAS", "▸ PERSONAS · 7", "naruto roster", "01 // shikamaru-nara"} {
		if !strings.Contains(view, want) {
			t.Fatalf("personas at %dx%d missing %q\n%s", width, height, want, view)
		}
	}
	if width < 120 {
		return
	}
	for _, want := range []string{"kakashi-hatake", "0 cmd"} {
		if !strings.Contains(view, want) {
			t.Fatalf("personas at %dx%d missing %q\n%s", width, height, want, view)
		}
	}
}

func TestStudioPersonasInspectorFoci(t *testing.T) {
	t.Parallel()

	cases := []struct {
		slug   string
		want   []string
		scroll []string
	}{
		{"kakashi-hatake", []string{
			"kakashi-hatake", "Kakashi Hatake", "reviewer",
			"01   code-smells", "// RELATED", "NAME", "TYPE", "DETAIL",
			// The kicker has always counted the persona's laws; the table now
			// carries them, with the type column saying which is which.
			"project-scope-only", "law",
		}, []string{"No commands bind this persona."}},
		{"shikamaru-nara", []string{
			"shikamaru-nara", "Shikamaru Nara", "concierge",
			"01   pdca-cycle", "// RELATED",
		}, []string{"No commands bind this persona."}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.slug, func(t *testing.T) {
			t.Parallel()
			assertPersonaInspectorFocus(t, tc.slug, tc.want, tc.scroll)
		})
	}
}

func assertPersonaInspectorFocus(t *testing.T, slug string, want, scroll []string) {
	t.Helper()
	screen, frame := personasScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range personasFocusKeys(slug) {
		screen = studioDrive(t, screen, frame, "down")
	}
	view := screentest.StripANSI(screen.View(frame))
	for _, text := range want {
		if !strings.Contains(view, text) {
			t.Fatalf("focus %s missing %q\n%s", slug, text, view)
		}
	}
	if len(scroll) == 0 {
		return
	}
	// Two tabs: the persona's fields, then the RELATED list this scrolls.
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	missing := append([]string{}, scroll...)
	var last string
	for i := 0; i < 80 && len(missing) > 0; i++ {
		last = screentest.StripANSI(screen.View(frame))
		var still []string
		for _, text := range missing {
			if !strings.Contains(last, text) {
				still = append(still, text)
			}
		}
		missing = still
		if len(missing) == 0 {
			break
		}
		screen = studioDrive(t, screen, frame, "j")
	}
	if len(missing) > 0 {
		t.Fatalf("focus %s scrolled inspector missing %v\n%s", slug, missing, last)
	}
}

func TestStudioPersonasDoesNotMutatePersonaMarkdown(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps, err := StudioPersonasDeps(root)
	if err != nil {
		t.Fatal(err)
	}
	before := hashPersonaMarkdown(t, deps.Editor.RootDir())
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(screenhost.StudioPersonas, deps)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for _, key := range []string{"j", "j", "k", "tab", "j", "tab", "ctrl+s", "esc"} {
		screen = studioDrive(t, screen, frame, key)
	}
	after := hashPersonaMarkdown(t, deps.Editor.RootDir())
	if before != after {
		t.Fatal("driving studio.personas mutated personas/*.md")
	}
}

func hashPersonaMarkdown(tb testing.TB, root string) string {
	tb.Helper()
	dir := filepath.Join(root, config.EntityKindPersona.Folder())
	entries, err := os.ReadDir(dir)
	if err != nil {
		tb.Fatal(err)
	}
	sum := sha256.New()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := io.Copy(sum, f); err != nil {
			_ = f.Close()
			tb.Fatal(err)
		}
		_ = f.Close()
	}
	return fmt.Sprintf("%x", sum.Sum(nil))
}

func TestStudioPersonasInspectorEnterOpensSkillEntity(t *testing.T) {
	t.Parallel()

	screen, frame := personasScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range personasFocusKeys("kakashi-hatake") {
		screen = studioDrive(t, screen, frame, "down")
	}
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	if got := screen.grid.Focus(); got != sectionPersonasRelated {
		t.Fatalf("focus = %q, want the RELATED zone", got)
	}
	persona := screen.studioPersonaIndex
	outcome := screen.Update(frame, screentest.Key("enter"))
	next, ok := outcome.Screen.(Screen)
	if !ok {
		t.Fatalf("enter carried %T", outcome.Screen)
	}
	if next.studioPersonaIndex != persona {
		t.Fatalf("enter moved persona index from %d to %d", persona, next.studioPersonaIndex)
	}
	if outcome.Action.Kind != screenhost.ActionOpenEntity {
		t.Fatalf("enter action = %v, want ActionOpenEntity", outcome.Action.Kind)
	}
	if outcome.Action.EntityKind != personaRelatedSkillKind {
		t.Fatalf("EntityKind = %q, want %q", outcome.Action.EntityKind, personaRelatedSkillKind)
	}
	if outcome.Action.Value != "code-smells" {
		t.Fatalf("Value = %q, want code-smells", outcome.Action.Value)
	}
}

func TestStudioPersonasInspectorEnterOpensLawEntity(t *testing.T) {
	t.Parallel()

	screen, frame := personasScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range personasFocusKeys("kakashi-hatake") {
		screen = studioDrive(t, screen, frame, "down")
	}
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	related := screen.personaRelatedRows(studioPersonaRows(studioPersonasGoldenBundle(), nil)[screen.studioPersonaIndex])
	first := -1
	for i, row := range related {
		if row.Kind == relatedLaw {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("kakashi has no law rows; nothing to open")
	}
	for range first {
		screen = studioDrive(t, screen, frame, "j")
	}
	outcome := screen.Update(frame, screentest.Key("enter"))
	if outcome.Action.Kind != screenhost.ActionOpenEntity {
		t.Fatalf("enter action = %v, want ActionOpenEntity", outcome.Action.Kind)
	}
	if outcome.Action.EntityKind != personaRelatedLawKind {
		t.Fatalf("EntityKind = %q, want %q", outcome.Action.EntityKind, personaRelatedLawKind)
	}
	if outcome.Action.Value != "project-scope-only" {
		t.Fatalf("Value = %q, want project-scope-only", outcome.Action.Value)
	}
}

func TestStudioPersonasNoEmbeddedCommands(t *testing.T) {
	t.Parallel()
	rows := studioPersonaRows(studioPersonasGoldenBundle(), nil)
	for _, row := range rows {
		if len(row.Commands) != 0 {
			t.Fatalf("persona %s has embedded commands: %+v", row.Persona.Slug, row.Commands)
		}
	}
}

func TestStudioPersonasListEnterOpensSelectedPersona(t *testing.T) {
	t.Parallel()

	screen, frame := personasScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if got := screen.grid.Focus(); got != sectionPersonasList {
		t.Fatalf("entry focus = %q, want list", got)
	}
	for range personasFocusKeys("naruto-uzumaki") {
		screen = studioDrive(t, screen, frame, "down")
	}
	if screen.studioPersonaIndex == 0 {
		t.Fatal("test setup left selection on the first persona")
	}
	outcome := screen.Update(frame, screentest.Key("enter"))
	if outcome.Action.Kind != screenhost.ActionOpenEntity {
		t.Fatalf("list enter action = %v, want ActionOpenEntity", outcome.Action.Kind)
	}
	if outcome.Action.EntityKind != "personas" {
		t.Fatalf("EntityKind = %q, want personas", outcome.Action.EntityKind)
	}
	if outcome.Action.Value != "naruto-uzumaki" {
		t.Fatalf("Value = %q, want naruto-uzumaki", outcome.Action.Value)
	}
}

func TestStudioPersonasInspectorJKMovesRelatedNotPersona(t *testing.T) {
	t.Parallel()

	screen, frame := personasScreen(t, 120, 40, State{})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for range personasFocusKeys("kakashi-hatake") {
		screen = studioDrive(t, screen, frame, "down")
	}
	screen = studioDrive(t, screen, frame, "tab")
	screen = studioDrive(t, screen, frame, "tab")
	persona := screen.studioPersonaIndex
	if screen.studioPersonaRelatedIndex != 0 {
		t.Fatalf("related index = %d, want 0", screen.studioPersonaRelatedIndex)
	}
	screen = studioDrive(t, screen, frame, "j")
	if screen.studioPersonaIndex != persona {
		t.Fatalf("inspector j moved persona index from %d to %d", persona, screen.studioPersonaIndex)
	}
	if screen.studioPersonaRelatedIndex != 1 {
		t.Fatalf("inspector j related index = %d, want 1", screen.studioPersonaRelatedIndex)
	}
}

func TestStudioPersonasRelatedEmptyCopy(t *testing.T) {
	t.Parallel()

	persona := config.Persona{Slug: "solo", Name: "Solo", SchemaVersion: 2}
	bundle := config.Bundle{Personas: []config.Persona{persona}, AllPersonas: []config.Persona{persona}}
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(screenhost.StudioPersonas, Deps{
		Ctx:      context.Background(),
		Snapshot: config.BuildSnapshot(bundle),
	}).withFrame(frame)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	view := screentest.StripANSI(screen.View(frame))
	for _, want := range []string{
		"No skills in this repertoire.",
		"No commands bind this persona.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty related missing %q\n%s", want, view)
		}
	}
}

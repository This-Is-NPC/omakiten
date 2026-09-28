package studio

import (
	"strings"
	"testing"
	"unicode"

	"omakiten/internal/config"
	"omakiten/internal/testutil"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

const personaRelatedControlPayload = "漢字 \x1b[31mESC\x1b]0;OSC\a C0\x00 C1\u009b31m\u009d end"

func TestStudioPersonasRelatedProjectionRenderGoldens(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		width, height   int
		empty, selected bool
	}{
		"compact":       {80, 24, false, false},
		"wide-selected": {200, 50, false, true},
		"empty":         {80, 24, true, false},
		"unicode":       {120, 40, false, false},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			view := renderPersonaRelatedSecurityFixture(t, tc.width, tc.height, tc.empty, tc.selected)
			assertPersonaRelatedRenderSafe(t, view)
			testutil.Golden(t, "personas-related-"+name+".golden", view)
		})
	}
}

func renderPersonaRelatedSecurityFixture(t *testing.T, width, height int, empty, selected bool) string {
	t.Helper()
	text := func(key string) string {
		switch key {
		case "tui.studio.personas.related.kind.skill", "tui.studio.personas.related.kind.law", "tui.studio.personas.related.kind.command":
			return personaRelatedControlPayload
		case "tui.studio.personas.related.skills_empty", "tui.studio.personas.related.commands_empty":
			return personaRelatedControlPayload
		case "tui.studio.personas.skills_ratio":
			return personaRelatedControlPayload + " %d/%d"
		case "tui.studio.personas.known":
			return personaRelatedControlPayload
		default:
			return ""
		}
	}
	frame := screenhost.NewFrame(screenhost.FrameOptions{
		Width: width, Height: height, ProjectID: 1, ProjectSlug: "omakiten",
		ChromeRows: screentest.DefaultChromeRows, Styles: screentest.Styles(),
		Markdown: screentest.MarkdownTokens(), Text: text,
	})
	bundle := personaRelatedSecurityBundle(empty)
	state := State{}
	if selected {
		state.PersonaRelatedIndex = 1
	}
	screen := New().Bind(screenhost.StudioPersonas, Deps{
		Snapshot:     config.BuildSnapshot(bundle),
		CommandNames: []string{personaRelatedControlPayload},
	}).WithState(state)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if len(screen.personaRelatedRows(screen.projection.Personas[0])) > 0 {
		for range 2 {
			screen = screen.Update(frame, screentest.Key("tab")).Screen.(Screen)
		}
	}
	return screentest.StripANSI(screen.View(frame))
}

func personaRelatedSecurityBundle(empty bool) config.Bundle {
	persona := config.Persona{Slug: "builder"}
	if !empty {
		persona.SkillRepertoire = []string{personaRelatedControlPayload}
		persona.Laws = []string{personaRelatedControlPayload}
	}
	bundle := config.Bundle{
		Personas:    []config.Persona{persona},
		AllPersonas: []config.Persona{persona},
	}
	if !empty {
		bundle.Laws = []config.Law{{Slug: personaRelatedControlPayload, Severity: personaRelatedControlPayload}}
		bundle.AllLaws = append([]config.Law(nil), bundle.Laws...)
		bundle.MCPCommands = map[string]config.MCPCommandSpec{
			personaRelatedControlPayload: {Persona: persona.Slug, Skills: []string{personaRelatedControlPayload}},
		}
	}
	return bundle
}

func assertPersonaRelatedRenderSafe(t *testing.T, view string) {
	t.Helper()
	if !strings.Contains(view, "漢字") {
		t.Fatalf("render lost harmless Unicode\n%s", view)
	}
	for _, r := range view {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("render retained terminal control U+%04X\n%s", r, view)
		}
	}
	if strings.Contains(view, "owned") {
		t.Fatalf("render retained OSC payload\n%s", view)
	}
	if !strings.Contains(view, screenkit.Sanitize(personaRelatedControlPayload)) {
		t.Fatalf("render lost sanitized relationship text\n%s", view)
	}
}

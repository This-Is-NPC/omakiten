package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/entitylist"
	"omakiten/internal/tui/screens/studio"
)

func renderStudioTest(m Model, id screenhost.ID) string {
	m.repos.ResolveCommandPreview = agentruntime.ResolveCommandPreview
	frame := m.screenFrame()
	screen := m.boundStudioScreen(id)
	out := screen.Lifecycle(frame, screenhost.LifecycleEnter)
	next, ok := out.Screen.(studio.Screen)
	if !ok {
		panic("renderStudioTest: Lifecycle did not carry a studio.Screen")
	}
	return pumpStudioCmd(next, frame, out.Command).View(frame)
}

func pumpStudioCmd(screen studio.Screen, frame screenhost.Frame, cmd tea.Cmd) studio.Screen {
	for i := 0; cmd != nil && i < 8; i++ {
		msg := cmd()
		if msg == nil {
			return screen
		}
		if next, ok := msg.(tea.Cmd); ok {
			cmd = next
			continue
		}
		out := screen.Update(frame, msg)
		next, ok := out.Screen.(studio.Screen)
		if !ok {
			return screen
		}
		screen = next
		cmd = out.Command
	}
	return screen
}

func updateStudioTest(m *Model, id screenhost.ID, msg tea.KeyMsg) screenhost.Outcome {
	outcome := m.boundStudioScreen(id).Update(m.screenFrame(), msg)
	m.studioScreen = outcome.Screen.(studio.Screen)
	return outcome
}

func studioState(draft studio.StudioDraft, mutate func(*studio.State)) studio.Screen {
	state := studio.State{Draft: draft}
	if mutate != nil {
		mutate(&state)
	}
	return studio.New().WithState(state)
}

func TestStudioTopOrderAndSubLabels(t *testing.T) {
	wantTop := []screenhost.TopID{screenhost.TopTasks, screenhost.TopStats, screenhost.TopStudio, screenhost.TopSettings}
	if len(topOrder) != len(wantTop) {
		t.Fatalf("topOrder len = %d, want %d", len(topOrder), len(wantTop))
	}
	for i, want := range wantTop {
		if topOrder[i] != want {
			t.Fatalf("topOrder[%d] = %s, want %s", i, topOrder[i], want)
		}
	}

	wantSubs := []struct {
		sub   screenhost.ID
		label string
	}{
		{screenhost.StudioWorkflow, "workflow"},
		{screenhost.StudioCommands, "commands"},
		{screenhost.StudioPersonas, "personas"},
		{screenhost.StudioHooks, "hooks"},
	}
	studioSubs := subsByTop[screenhost.TopStudio]
	if len(studioSubs) != len(wantSubs) {
		t.Fatalf("Studio sub count = %d, want %d", len(studioSubs), len(wantSubs))
	}
	for i, want := range wantSubs {
		if studioSubs[i] != want.sub {
			t.Fatalf("Studio sub[%d] = %s, want %s", i, studioSubs[i], want.sub)
		}
		if subLabels[want.sub] != want.label {
			t.Fatalf("Studio sub label %s = %q, want %q", want.sub, subLabels[want.sub], want.label)
		}
	}
}

func TestStudioSubCycleAndSettingsEntityIsolation(t *testing.T) {
	m := Model{navigation: screenhost.StudioWorkflow}
	m.cycleSub(1)
	if m.navigationTop() != screenhost.TopStudio || m.navigation != screenhost.StudioCommands {
		t.Fatalf("Studio '/' cycle = (%s, %s), want (screenhost.TopStudio, screenhost.StudioCommands)", m.navigationTop(), m.navigation)
	}
	m.cycleSub(-1)
	if m.navigation != screenhost.StudioWorkflow {
		t.Fatalf("Studio ',' cycle = %s, want screenhost.StudioWorkflow", m.navigation)
	}
	m.cycleSub(-1)
	if m.navigation != screenhost.StudioHooks {
		t.Fatalf("Studio reverse wrap = %s, want screenhost.StudioHooks", m.navigation)
	}

	for _, id := range []screenhost.ID{screenhost.StudioCommands, screenhost.StudioWorkflow, screenhost.StudioPersonas, screenhost.StudioHooks} {
		if _, ok := entitylist.ForID(id); ok {
			t.Fatalf("Studio screen %q mapped as Settings entity", id)
		}
	}
}

func TestStudioFlowWarningsCoverDisconnectedFinalPath(t *testing.T) {
	warnings := studio.StudioFlowWarnings(config.Workflow{Buckets: []config.Bucket{
		{ID: 1, Key: "backlog", Position: 1},
		{ID: 2, Key: "review", Position: 2},
		{ID: 3, Key: "done", Position: 3},
	}, Transitions: []config.Transition{{From: 1, To: 2}, {From: 3, To: 1}}}, nil)

	assertContains(t, warnings, "non-final bucket review has no outbound transitions")
	assertContains(t, warnings, "no path from backlog to final bucket done")
}

func TestStudioCommandsRendersGlobalAndWarnings(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Laws = []config.Law{{Slug: "safety"}}
	bundle.AllLaws = []config.Law{{Slug: "safety"}}
	bundle.Templates = []config.TaskTemplate{{Slug: "requirements"}}
	bundle.AllTemplates = []config.TaskTemplate{{Slug: "requirements"}}
	bundle.Commands = map[string]config.CommandSpec{
		config.CommandsGlobalKey: {Laws: []string{"safety"}},
		"okt-task-continue":      {Persona: "missing-persona", Skills: []string{"ghost-skill"}, Laws: []string{"missing-law"}, Templates: []string{"missing-template"}},
	}
	draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{styles: newStyles(tuiTestTheme()), width: 120, height: 40, studioScreen: studioState(draft, nil)}

	view := ansi.Strip(renderStudioTest(m, screenhost.StudioCommands))
	for _, want := range []string{
		"COMMANDS",
		"global laws",
		"safety",
		"okt-task-continue",
		"commands.okt-task-continue.persona",
		"commands.okt-task-continue.skills",
		"commands.okt-task-continue.laws",
		"commands.okt-task-continue.templates",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("Studio commands missing %q\n%s", want, view)
		}
	}
}

func TestStudioCommandsPersonaChangeSurfacesInvalidSkills(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Personas = []config.Persona{{Slug: "builder", SkillRepertoire: []string{"code"}}, {Slug: "reviewer", SkillRepertoire: []string{"review"}}}
	bundle.AllPersonas = append([]config.Persona(nil), bundle.Personas...)
	bundle.Skills = []config.Skill{{Slug: "code"}, {Slug: "review"}}
	bundle.AllSkills = append([]config.Skill(nil), bundle.Skills...)
	bundle.Commands = map[string]config.CommandSpec{"okt-task-continue": {Persona: "builder", Skills: []string{"code"}}}
	draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{styles: newStyles(tuiTestTheme()), width: 120, height: 40, studioScreen: studioState(draft, func(state *studio.State) {
		state.CommandIndex = studio.CommandIndexFor(bundle.Commands, "okt-task-continue")
	})}

	updateStudioTest(&m, screenhost.StudioCommands, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})

	report := draft.Report()
	if report.ValidationError == nil || !strings.Contains(report.ValidationError.Error(), "not in persona \"reviewer\" skill_repertoire") {
		t.Fatalf("validation error = %v, want invalid skill subset", report.ValidationError)
	}
	view := ansi.Strip(renderStudioTest(m, screenhost.StudioCommands))
	if !strings.Contains(view, "Validation: commands.okt-task-continue.skills") {
		t.Fatalf("Studio commands did not render blocking validation\n%s", view)
	}
}

func TestStudioCommandsGlobalLawMutation(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Laws = []config.Law{{Slug: "safety"}}
	bundle.AllLaws = []config.Law{{Slug: "safety"}}
	draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{styles: newStyles(tuiTestTheme()), studioScreen: studioState(draft, nil)}

	updateStudioTest(&m, screenhost.StudioCommands, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})

	laws := draft.Report().Candidate.Commands[config.CommandsGlobalKey].Laws
	if len(laws) != 1 || laws[0] != "safety" {
		t.Fatalf("global laws = %#v, want safety", laws)
	}
}

func TestStudioPreviewRendersCandidatePrompt(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.LanguageSettings.AgentOutput = "English"
	bundle.Personas = []config.Persona{{Slug: "builder", Name: "Builder", Description: "Builds safely", Body: "Ship working code.", SkillRepertoire: []string{"code"}, Laws: []string{"persona-law"}}}
	bundle.Skills = []config.Skill{{Slug: "code", Name: "Code", Body: "Implement the change."}}
	bundle.Laws = []config.Law{{Slug: "global-law", Name: "Global", Severity: "warning", Body: "Global rule."}, {Slug: "persona-law", Name: "Persona", Severity: "error", Body: "Persona rule."}, {Slug: "template-law", Name: "Template", Severity: "info", Body: "Template rule."}, {Slug: "disabled-law", Name: "Disabled", Severity: "info", Body: "Disabled rule."}}
	bundle.Templates = []config.TaskTemplate{{Slug: "task-template", Name: "Task Template", Default: "task", Description: "Task scaffold", Laws: []string{"template-law", "disabled-law"}, Body: "Template body."}}
	bundle.Commands = map[string]config.CommandSpec{
		config.CommandsGlobalKey: {Laws: []string{"global-law", "disabled-law"}},
		"okt-task-continue":      {Persona: "builder", Skills: []string{"code"}, Templates: []string{"task-template"}, LawsDisabled: []string{"disabled-law"}},
	}
	draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{styles: newStyles(tuiTestTheme()), width: 120, height: 40, studioScreen: studioState(draft, func(state *studio.State) {
		state.CommandIndex = studio.CommandIndexFor(bundle.Commands, "okt-task-continue")
	})}

	view := ansi.Strip(renderStudioTest(m, screenhost.StudioCommands))
	for _, want := range []string{
		"COMMANDS",
		"PREVIEW",
		"okt-task-continue",
		"## Persona",
		"Ship working code.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("Studio preview chrome missing %q\n%s", want, view)
		}
	}
	m.repos.ResolveCommandPreview = agentruntime.ResolveCommandPreview
	preview := ansi.Strip(m.boundStudioScreen(screenhost.StudioCommands).PromptPreview(m.screenFrame(), draft.Report().Candidate))
	for _, want := range []string{
		"## Skills",
		"Implement the change.",
		"## Laws",
		"Global rule.",
		"Persona rule.",
		"Template rule.",
		"## Templates",
		"task-template",
		"Output language:** English",
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("Studio preview prompt missing %q\n%s", want, preview)
		}
	}
	if strings.Contains(view, "Disabled rule.") {
		t.Fatalf("Studio preview rendered disabled law\n%s", view)
	}
}

func TestStudioCommandsCtrlSOpensApplyOverlayWithoutApplying(t *testing.T) {
	store := &studioDraftStore{bundle: studioDraftBundle()}
	draft, err := bundledraft.New(bundleeditor.New(store, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	draft.RenameBucket(1, "Inbox")
	screen := studioState(draft, nil).Bind(screenhost.StudioCommands, studio.Deps{})
	outcome := screen.Update(screenhost.Frame{}, tea.KeyMsg{Type: tea.KeyCtrlS})
	next, ok := outcome.Screen.(studio.Screen)
	if !ok {
		t.Fatalf("Update carried %T", outcome.Screen)
	}
	if outcome.Action.Kind == screenhost.ActionNavigate {
		t.Fatalf("Commands ctrl+s navigated to %s", outcome.Action.Target)
	}
	if !next.ApplyOverlayOpen() {
		t.Fatal("Commands ctrl+s did not open the apply overlay")
	}
	if store.saves != 0 {
		t.Fatalf("Commands ctrl+s applied candidate, saves=%d", store.saves)
	}
}

func TestStudioHostedScreenOwnsOnlyHandledKeys(t *testing.T) {
	commands := studio.New().Bind(screenhost.StudioCommands, studio.Deps{})
	for _, key := range []string{"p", "w", "t", "x"} {
		if !commands.OwnsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}) {
			t.Fatalf("Commands does not own handled key %q", key)
		}
	}
	if commands.OwnsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}) {
		t.Fatal("Commands owns unhandled key e")
	}
}

func TestStudioPromptPreviewSanitizesControlsAndPreservesMarkdownLines(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Personas = []config.Persona{{Slug: "builder", Name: "Builder", Body: "first\nsecond\x00\x1b]0;owned\a\u009b31m"}}
	bundle.AllPersonas = append([]config.Persona(nil), bundle.Personas...)
	bundle.Commands = map[string]config.CommandSpec{"okt-task-continue": {Persona: "builder"}}
	draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{styles: newStyles(tuiTestTheme()), studioScreen: studioState(draft, func(state *studio.State) {
		state.CommandIndex = studio.CommandIndexFor(bundle.Commands, "okt-task-continue")
	})}
	m.repos.ResolveCommandPreview = agentruntime.ResolveCommandPreview
	view := ansi.Strip(m.boundStudioScreen(screenhost.StudioCommands).PromptPreview(m.screenFrame(), draft.Report().Candidate))
	if !strings.Contains(view, "first\nsecond") {
		t.Fatalf("prompt Markdown line feeds were not preserved:\n%s", view)
	}
	for _, r := range view {
		if r != '\n' && (r < 0x20 || r >= 0x7f && r <= 0x9f) {
			t.Fatalf("prompt retained terminal control U+%04X in %q", r, view)
		}
	}
}

func TestStudioGuardValidationBlocksInvalidFields(t *testing.T) {
	cases := []struct {
		name  string
		guard config.TransitionGuard
		want  string
	}{
		{"unknown", config.TransitionGuard{Type: "unknown_type"}, "unknown guard type"},
		{"blockers_empty", config.TransitionGuard{Type: "blockers_in"}, "buckets is required"},
		{"blockers_bad_bucket", config.TransitionGuard{Type: "blockers_in", Buckets: []string{"missing"}}, "bucket key \"missing\" not found"},
		{"comments_min_count", config.TransitionGuard{Type: "comments_min", Count: 0}, "count must be >= 1"},
		{"comments_tagged_tag", config.TransitionGuard{Type: "comments_tagged", Count: 1}, "tag is required"},
		{"comments_tagged_count", config.TransitionGuard{Type: "comments_tagged", Tag: "review", Count: 0}, "count must be >= 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := studioDraftBundle()
			bundle.Workflows[0].Transitions[0].Guards = []config.TransitionGuard{{Type: "comments_min", Count: 1}}
			draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			report := draft.SetGuard(studio.StudioGuardSetTransition, 1, 2, 0, tc.guard)
			if report.ValidationError == nil || !strings.Contains(report.ValidationError.Error(), tc.want) {
				t.Fatalf("validation error = %v, want %q", report.ValidationError, tc.want)
			}
		})
	}
}

func TestStudioGuardReorderPreservesDeclarationOrder(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Workflows[0].Transitions[0].Guards = []config.TransitionGuard{{Type: "comments_min", Count: 1}, {Type: "comments_tagged", Tag: "review", Count: 1}}
	draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	report := draft.MoveGuard(studio.StudioGuardSetTransition, 1, 2, 0, 1)
	if report.ValidationError != nil {
		t.Fatalf("move guard validation error = %v", report.ValidationError)
	}
	guards := report.Candidate.Workflows[0].Transitions[0].Guards
	if guards[0].Type != "comments_tagged" || guards[1].Type != "comments_min" {
		t.Fatalf("guard order after move = %#v", guards)
	}
}

// A hosted screen's own asynchronous message reaches that screen.
//
// Studio › Commands resolves its prompt preview off the render thread and
// answers the keystroke with a tea.Cmd; the result comes back as a message the
// root does not recognise. The root's Update used to dispatch to the active
// screen only from inside its `case tea.KeyMsg` branch, so every such message
// fell through and was dropped — the preview painted "Composing preview…" for
// the rest of the session, and no test saw it because they all pumped the cmd
// straight back into the screen instead of through the root.
func TestRootForwardsAScreensOwnAsyncMessage(t *testing.T) {
	bundle := studioDraftBundle()
	bundle.Personas = []config.Persona{{Slug: "builder", Name: "Builder", Body: "Ship working code."}}
	bundle.Commands = map[string]config.CommandSpec{"okt-task-continue": {Persona: "builder"}}
	draft, err := bundledraft.New(bundleeditor.New(&studioDraftStore{bundle: bundle}, "omakiten.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{
		styles: newStyles(tuiTestTheme()), width: 120, height: 40,
		navigation: screenhost.StudioCommands,
		studioScreen: studioState(draft, func(state *studio.State) {
			state.CommandIndex = studio.CommandIndexFor(bundle.Commands, "okt-task-continue")
		}),
	}

	frame := m.screenFrame()
	m.repos.ResolveCommandPreview = agentruntime.ResolveCommandPreview
	out := m.boundStudioScreen(screenhost.StudioCommands).Lifecycle(frame, screenhost.LifecycleEnter)
	m.studioScreen = out.Screen.(studio.Screen)
	if out.Command == nil {
		t.Fatal("entering Commands issued no preview command; there is nothing to forward")
	}
	msg := out.Command()
	if msg == nil {
		t.Fatal("the preview command produced no message")
	}
	if _, isKey := msg.(tea.KeyMsg); isKey {
		t.Fatalf("the preview command produced a key message (%T); this test is about the other kind", msg)
	}

	next, _ := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update carried %T", next)
	}
	view := ansi.Strip(updated.boundStudioScreen(screenhost.StudioCommands).View(updated.screenFrame()))
	if strings.Contains(view, "Composing preview") {
		t.Fatalf("the preview is still composing after its own result was delivered\n%s", view)
	}
	if !strings.Contains(view, "Ship working code.") {
		t.Fatalf("the resolved preview did not reach the screen\n%s", view)
	}
}

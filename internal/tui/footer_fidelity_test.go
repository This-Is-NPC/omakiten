package tui

import (
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// newFooterFidelityModel builds a minimal Model wired to the bundled en
// catalog so footerTokens() resolves real labels. It mirrors the literal
// construction in footer_parity_test.go: the footer switch only reads
// m.t + the surface state flags, so the heavyweight model helpers are
// unnecessary here.
func newFooterFidelityModel(t *testing.T) Model {
	t.Helper()
	return Model{
		styles:    newStyles(config.Theme{}),
		width:     160,
		height:    40,
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
		repos:     Repositories{Catalog: newTestCatalog(t)},
	}
}

// footerKeyLabel returns the label advertised for the given key token,
// and whether the token is present at all. Multi-key tokens are matched
// on their exact key string (e.g. "ctrl+s", "j/k").
func footerKeyLabel(tokens []footerToken, key string) (string, bool) {
	for _, tok := range tokens {
		if tok.key == key {
			return tok.label, true
		}
	}
	return "", false
}

// footerHasFragment reports whether any token advertises the given key
// fragment (splitting "j/k" → "j","k" etc.). Used to assert a key is or
// is not surfaced regardless of how it is grouped into a token.
func footerHasFragment(tokens []footerToken, fragment string) bool {
	for _, tok := range tokens {
		for _, frag := range splitKeyToken(tok.key) {
			if frag == fragment {
				return true
			}
		}
	}
	return false
}

// TestPlanGoalEditFooterAdvertisesCtrlS pins AC #1: the plan goal editor
// (modePlanGoal) is a full-panel textarea where Enter inserts a newline
// and ctrl+s saves (input.go). The footer must advertise ctrl+s as save
// and never imply that a bare Enter saves.
func TestPlanGoalEditFooterAdvertisesCtrlS(t *testing.T) {
	t.Parallel()
	m := newFooterFidelityModel(t)
	m.mode = modePlanGoal
	m.planNetworkOpen = true
	m.sub = subPlans

	tokens := m.footerTokens()

	label, ok := footerKeyLabel(tokens, "ctrl+s")
	if !ok {
		t.Fatalf("plan goal footer must advertise ctrl+s save; tokens=%v", tokens)
	}
	if label != m.t("tui.footer.save") {
		t.Fatalf("ctrl+s label = %q, want save", label)
	}

	// Newline must be advertised on a modifier-Enter token, and a bare
	// "enter" token must NOT claim "save" (the AC #1 mismatch).
	if !footerHasFragment(tokens, "alt+enter") && !footerHasFragment(tokens, "shift+enter") {
		t.Fatalf("plan goal footer must advertise modifier-enter newline; tokens=%v", tokens)
	}
	for _, tok := range tokens {
		if tok.key == "enter" {
			t.Fatalf("plan goal footer must not advertise a bare enter token (Enter inserts a newline here); got %v", tok)
		}
	}
}

// TestCommentEditFooterOmitsHelp pins AC #2: while editing a comment
// (commentScreenEditing), the global help key (`?`) is gated off in
// model.go (it is a literal character typed into the textarea). The
// footer must not advertise `?` in that state.
func TestCommentEditFooterOmitsHelp(t *testing.T) {
	t.Parallel()
	m := newFooterFidelityModel(t)
	m.commentScreenOpen = true
	m.commentScreenEditing = true

	tokens := m.footerTokens()

	if footerHasFragment(tokens, "?") {
		t.Fatalf("comment-edit footer must not advertise ? help (unreachable while editing); tokens=%v", tokens)
	}
	if _, ok := footerKeyLabel(tokens, "ctrl+s"); !ok {
		t.Fatalf("comment-edit footer must still advertise ctrl+s save; tokens=%v", tokens)
	}
}

// TestEntityViewFooterKindAware pins AC #3: the entity detail footer
// advertises only the actions valid for the open entity kind.
//   - `d` (arm delete): law / skill / persona, NOT template (template's
//     `d` is a no-op status hint in entity_screen.go).
//   - `p` (skill picker): persona only.
//   - `a` (set default): template only.
func TestEntityViewFooterKindAware(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		kind      entityKind
		wantDel   bool // `d` advertised
		wantP     bool // `p` advertised
		wantADef  bool // `a` advertised
	}{
		{name: "law", kind: entityKindLaw, wantDel: true, wantP: false, wantADef: false},
		{name: "skill", kind: entityKindSkill, wantDel: true, wantP: false, wantADef: false},
		{name: "persona", kind: entityKindPersona, wantDel: true, wantP: true, wantADef: false},
		{name: "template", kind: entityKindTemplate, wantDel: false, wantP: false, wantADef: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newFooterFidelityModel(t)
			m.entityScreen = entityScreenView
			m.entityForm = entityForm{kind: tc.kind, mode: entityScreenView}

			tokens := m.footerTokens()

			// Edit-in-editor is valid for every kind.
			if _, ok := footerKeyLabel(tokens, "e"); !ok {
				t.Fatalf("%s entity footer must advertise e (edit); tokens=%v", tc.name, tokens)
			}
			if got := footerHasFragment(tokens, "d"); got != tc.wantDel {
				t.Errorf("%s: d advertised=%v, want %v; tokens=%v", tc.name, got, tc.wantDel, tokens)
			}
			if got := footerHasFragment(tokens, "p"); got != tc.wantP {
				t.Errorf("%s: p advertised=%v, want %v; tokens=%v", tc.name, got, tc.wantP, tokens)
			}
			if got := footerHasFragment(tokens, "a"); got != tc.wantADef {
				t.Errorf("%s: a advertised=%v, want %v; tokens=%v", tc.name, got, tc.wantADef, tokens)
			}
		})
	}
}

// TestPlanNetworkEnterLabelByRowType pins AC #4: in the plan-network
// outline Enter opens a task on a task-card row but toggles
// collapse/expand on a wave-header row (handlePlanNetworkKey). The
// footer must label Enter for whichever row the cursor is on.
func TestPlanNetworkEnterLabelByRowType(t *testing.T) {
	t.Parallel()

	newPlanModel := func() Model {
		m := newFooterFidelityModel(t)
		m.sub = subPlans
		m.planNetworkOpen = true
		m.planNetworkShow = app.PlanShow{
			Plan: domain.Plan{ID: 1, Slug: "p1", Name: "Plan One"},
			Waves: []app.PlanWaveView{
				{
					Wave: domain.PlanWave{ID: 10, PlanID: 1, Name: "W1", Position: 1},
					Tasks: []domain.PlanTaskRow{
						{TaskID: 100, WaveID: 10, Title: "T1", BucketKey: "backlog"},
						{TaskID: 101, WaveID: 10, Title: "T2", BucketKey: "dev"},
					},
				},
			},
		}
		// Rows: [0]=wave header, [1]=task T1, [2]=task T2.
		m.planNetworkCursor = m.planNetworkCursor.WithItemCount(3)
		return m
	}

	// Cursor on the wave header (row 0) → Enter toggles the wave.
	mHeader := newPlanModel()
	mHeader.planNetworkCursor = mHeader.planNetworkCursor.SetCursor(0)
	headerLabel, ok := footerKeyLabel(mHeader.footerTokens(), "enter")
	if !ok {
		t.Fatalf("plan-network footer must advertise enter")
	}
	if headerLabel != mHeader.t("tui.footer.toggle_wave") {
		t.Errorf("enter on wave header = %q, want toggle-wave label %q", headerLabel, mHeader.t("tui.footer.toggle_wave"))
	}

	// Cursor on a task card (row 1) → Enter opens the task.
	mTask := newPlanModel()
	mTask.planNetworkCursor = mTask.planNetworkCursor.SetCursor(1)
	taskLabel, ok := footerKeyLabel(mTask.footerTokens(), "enter")
	if !ok {
		t.Fatalf("plan-network footer must advertise enter")
	}
	if taskLabel != mTask.t("tui.footer.open") {
		t.Errorf("enter on task row = %q, want open label %q", taskLabel, mTask.t("tui.footer.open"))
	}
}

// TestLogsFooterDefersFilterToChips pins AC #5: the Logs footer
// intentionally does NOT carry the `f`/`F` filter keys — discoverability
// lives in the chip strip (`(F cycle)` copy, covered by
// logs_filter_test.go). This locks that the footer is not silently
// re-cluttered with a duplicate filter hint.
func TestLogsFooterDefersFilterToChips(t *testing.T) {
	t.Parallel()
	m := newFooterFidelityModel(t)
	m.top = topStats
	m.sub = subStatsLogs

	tokens := m.footerTokens()

	if footerHasFragment(tokens, "f") || footerHasFragment(tokens, "F") {
		t.Fatalf("Logs footer must defer filter discoverability to the chip strip, not advertise f/F; tokens=%v", tokens)
	}
}

// TestTaskViewFooterFocusDependentEnterDelete pins AC #6: in the task
// detail view, Enter and delete (`d`) are focus-gated in
// handleTaskViewKey:
//   - form focus: `d` arms delete; Enter is a no-op.
//   - subtasks focus: Enter drills into a sub-task; `d` is inert.
//   - activity focus: Enter opens a comment; `d` is inert.
//
// The footer must match: advertise `d` only on form focus, and label
// Enter only where it acts.
func TestTaskViewFooterFocusDependentEnterDelete(t *testing.T) {
	t.Parallel()

	t.Run("form focus arms delete, hides enter", func(t *testing.T) {
		t.Parallel()
		m := newFooterFidelityModel(t)
		m.taskScreen = taskScreenView
		m.taskFocus = taskFocusForm

		tokens := m.footerTokens()
		if !footerHasFragment(tokens, "d") {
			t.Errorf("form focus must advertise d (arm delete); tokens=%v", tokens)
		}
		if footerHasFragment(tokens, "enter") {
			t.Errorf("form focus must not advertise enter (no-op there); tokens=%v", tokens)
		}
	})

	t.Run("subtasks focus opens sub-task, hides delete", func(t *testing.T) {
		t.Parallel()
		m := newFooterFidelityModel(t)
		m.taskScreen = taskScreenView
		m.taskFocus = taskFocusSubtasks

		tokens := m.footerTokens()
		if footerHasFragment(tokens, "d") {
			t.Errorf("subtasks focus must not advertise d (delete is form-only); tokens=%v", tokens)
		}
		label, ok := footerKeyLabel(tokens, "enter")
		if !ok {
			t.Fatalf("subtasks focus must advertise enter; tokens=%v", tokens)
		}
		if label != m.t("tui.footer.open_subtask") {
			t.Errorf("subtasks enter label = %q, want open-subtask", label)
		}
	})

	t.Run("activity focus opens comment, hides delete", func(t *testing.T) {
		t.Parallel()
		m := newFooterFidelityModel(t)
		m.taskScreen = taskScreenView
		m.taskFocus = taskFocusActivity

		tokens := m.footerTokens()
		if footerHasFragment(tokens, "d") {
			t.Errorf("activity focus must not advertise d (delete is form-only); tokens=%v", tokens)
		}
		label, ok := footerKeyLabel(tokens, "enter")
		if !ok {
			t.Fatalf("activity focus must advertise enter; tokens=%v", tokens)
		}
		if label != m.t("tui.footer.open_comment_activity") {
			t.Errorf("activity enter label = %q, want open-comment-activity", label)
		}
	})
}

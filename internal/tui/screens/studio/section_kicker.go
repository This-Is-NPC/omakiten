package studio

import (
	"fmt"
	"strings"

	"omakiten/internal/tui/components/screenlayout"
)

// sectionKicker paints a zone label the way task detail does: unfocused
// `// LABEL` in Info, focused `▸ LABEL` in HintAccent. The ▸ prefix is
// chrome, not catalog copy — same literal as taskdetail/render.go.
func (m Screen) sectionKicker(label string, focused bool) string {
	if focused {
		return m.styles.HintAccent.Render("▸ " + strings.ToUpper(label))
	}
	return m.styles.Kicker(label)
}

// sectionKickerCount is sectionKicker with a trailing count, matching
// task detail's focused activity header (`▸ ACTIVITY · N`).
func (m Screen) sectionKickerCount(label string, count int, focused bool) string {
	if focused {
		return m.styles.HintAccent.Render(fmt.Sprintf("▸ %s · %d", strings.ToUpper(label), count))
	}
	return m.styles.KickerCount(label, count)
}

// sectionKickerKeep accents copy that is not a Kicker (OPEN:, // HISTORY).
// Unfocused returns text unchanged so existing inspector chrome stays put.
func (m Screen) sectionKickerKeep(text string, focused bool) string {
	if !focused || text == "" {
		return text
	}
	return m.styles.HintAccent.Render("▸ " + strings.ToUpper(strings.TrimPrefix(text, "// ")))
}

func (m Screen) zoneFocused(id screenlayout.ID) bool {
	return m.grid.Focus() == id
}

func (m Screen) paintInspectorKicker(kicker string, focused bool) string {
	if kicker == "" || strings.Contains(kicker, "▸") || strings.ContainsRune(kicker, '\x1b') {
		return kicker
	}
	return m.sectionKickerKeep(kicker, focused)
}

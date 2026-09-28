package tui

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
)

func TestGlobalHeaderRouteSanitizesProjectSlug(t *testing.T) {
	t.Parallel()
	const hostile = "project 漢字 \x1b[31mred\x1b]0;owned\a \x00\u009b31m\u009d"
	m := Model{
		styles:     newStyles(config.Theme{}),
		width:      120,
		navigation: screenhost.TasksBoard,

		project: domain.ProjectContext{Slug: hostile},
	}
	plain := ansi.Strip(m.renderHeader())
	if strings.Contains(plain, "owned") {
		t.Fatalf("global header retained OSC payload: %q", plain)
	}
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("global header retained control U+%04X: %q", r, plain)
		}
	}
	if !strings.Contains(plain, "漢字") {
		t.Fatalf("global header lost harmless Unicode: %q", plain)
	}
}

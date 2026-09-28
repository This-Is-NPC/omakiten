package plannetwork

import (
	"regexp"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/screenkit"
)

var ansiSequencePattern = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func stripANSI(value string) string { return ansiSequencePattern.ReplaceAllString(value, "") }

func newStyles(config.Theme) screenkit.Styles {
	muted := lipgloss.NewStyle()
	return screenkit.Styles{
		Hint: muted, HintAccent: muted, Success: muted, Info: muted,
		BadgeInfo: muted, BadgeBlocker: muted,
	}
}

func buildRefreshHotPathModel(testing.TB) Screen { return New() }

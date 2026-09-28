package screenfixture

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/screenhost"
)

type bindProbe struct {
	binds   int
	updates int
}

func (p bindProbe) ID() screenhost.ID { return "probe" }

func (p bindProbe) Update(screenhost.Frame, tea.Msg) screenhost.Outcome {
	p.updates++
	return screenhost.Stay(p, nil)
}

func (p bindProbe) View(screenhost.Frame) string { return "probe" }

func (p bindProbe) Footer(screenhost.Frame) []screenhost.FooterBinding { return nil }

func (p bindProbe) Help(screenhost.Frame) []screenhost.HelpGroup { return nil }

func (p bindProbe) Lifecycle(screenhost.Frame, screenhost.LifecycleEvent) screenhost.Outcome {
	return screenhost.Stay(p, nil)
}

func TestDriveRebindsBetweenKeysAndAfterNestedUpdates(t *testing.T) {
	frame, err := FrameAt(120, 40)
	if err != nil {
		t.Fatalf("build frame: %v", err)
	}
	probe := bindProbe{}
	got := Drive(Scenario{
		Name:  "bind lifecycle",
		Keys:  []string{"j", "k"},
		Build: func(screenhost.Frame) screenhost.Screen { return probe },
		Bind: func(screen screenhost.Screen) screenhost.Screen {
			current := screen.(bindProbe)
			current.binds++
			return current
		},
	}, frame)
	result := got.(bindProbe)
	if result.binds != 3 {
		t.Fatalf("Bind ran %d times, want once before each key and once after the final update", result.binds)
	}
	if result.updates != 2 {
		t.Fatalf("Update ran %d times, want one per key", result.updates)
	}
}

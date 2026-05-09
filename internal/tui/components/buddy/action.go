package buddy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// ActionName is the canonical name registered with the hooks engine.
const ActionName = "buddy.show"

// ShowMsg is the message the action sends into the running tea.Program
// when an event matches a buddy.show hook. The TUI parent dispatches
// it by constructing a buddy.Model on top of Buddy + the current
// theme.
type ShowMsg struct {
	Buddy           config.Buddy
	Animation       string
	Text            string
	Position        Position
	Dismiss         DismissConfig
	TypingMsPerChar int
	FrameIntervalMs int
}

// BundleSnapshot is the narrow port the action needs from the bundle:
// the active buddy name plus the loaded buddies map. We pass a
// snapshot rather than the whole bundle so callers can refresh the
// snapshot on bundle reload without rebuilding the action.
type BundleSnapshot struct {
	ActiveBuddy string
	Buddies     map[string]config.Buddy
}

// ShowAction is the hooks.Action implementation for buddy.show. It
// holds an optional reference to the running tea.Program; CLI/MCP
// composition roots register the action with program=nil and the TUI
// composition root calls SetProgram once tea.NewProgram returns.
//
// The bundle snapshot is captured at construction; tests can swap it
// via SetBundle.
type ShowAction struct {
	mu       sync.RWMutex
	program  *tea.Program
	snapshot BundleSnapshot
}

// NewShowAction constructs a disconnected action. The TUI binds the
// program post-hoc; CLI/MCP runs leave program nil so Execute is a
// silent no-op.
func NewShowAction(snapshot BundleSnapshot) *ShowAction {
	return &ShowAction{snapshot: snapshot}
}

// Name satisfies hooks.Action.
func (a *ShowAction) Name() string { return ActionName }

// SetProgram links the action to a running tea.Program. Safe to call
// from the TUI composition root after tea.NewProgram returns.
func (a *ShowAction) SetProgram(p *tea.Program) {
	a.mu.Lock()
	a.program = p
	a.mu.Unlock()
}

// SetBundle refreshes the snapshot the action consults at execute
// time. Used after a config reload.
func (a *ShowAction) SetBundle(snapshot BundleSnapshot) {
	a.mu.Lock()
	a.snapshot = snapshot
	a.mu.Unlock()
}

// Execute parses the hook args, picks the active buddy, resolves the
// message, and sends a ShowMsg to the program. With no program (CLI/
// MCP) it returns nil so the hook records success without doing
// anything visible.
func (a *ShowAction) Execute(_ context.Context, ev domain.Event, args map[string]any) error {
	a.mu.RLock()
	program := a.program
	snapshot := a.snapshot
	a.mu.RUnlock()
	if program == nil {
		return nil
	}

	parsed, err := ParseArgs(args)
	if err != nil {
		return err
	}

	if snapshot.ActiveBuddy == "" {
		return fmt.Errorf("config.tui.buddy.active is empty — buddy.show needs an active buddy")
	}
	buddy, ok := snapshot.Buddies[snapshot.ActiveBuddy]
	if !ok {
		return fmt.Errorf("active buddy %q not loaded (check defaults/buddies/ + custom/)", snapshot.ActiveBuddy)
	}
	if _, ok := buddy.Animations[parsed.Animation]; !ok {
		return fmt.Errorf("buddy %q has no animation %q", buddy.Name, parsed.Animation)
	}

	text, err := resolveMessage(ev, parsed.MessageField)
	if err != nil {
		return err
	}

	frameInterval := parsed.FrameIntervalMs
	if frameInterval == 0 {
		frameInterval = buddy.FrameIntervalMs
	}

	program.Send(ShowMsg{
		Buddy:           buddy,
		Animation:       parsed.Animation,
		Text:            text,
		Position:        parsed.Position,
		Dismiss:         parsed.Dismiss,
		TypingMsPerChar: parsed.TypingMsPerChar,
		FrameIntervalMs: frameInterval,
	})
	return nil
}

// resolveMessage extracts the bubble text from the event using the
// message_field arg as a top-level key into ev.Payload (JSON). When
// the payload doesn't have a non-empty value at that key, falls back
// to ev.Body. An entirely empty result is an error so the buddy never
// pops up with a blank balloon.
func resolveMessage(ev domain.Event, field string) (string, error) {
	if ev.Payload != "" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(ev.Payload), &payload); err == nil {
			if raw, ok := payload[field]; ok {
				if s, ok := raw.(string); ok {
					if s != "" {
						return s, nil
					}
				}
			}
		}
	}
	if ev.Body != "" {
		return ev.Body, nil
	}
	return "", fmt.Errorf("event has no body and payload[%q] is empty — refusing to show empty buddy", field)
}

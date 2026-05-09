package tui

import "github.com/charmbracelet/bubbles/key"

// commentInputBindings is the single source of truth for the modal
// comment-input keystrokes. The bindings drive both the textarea's
// runtime KeyMap (newline insertion modifiers) and the help overlay's
// "Comment input" group, so help text and active handlers cannot
// drift apart. `Save` and `Cancel` are intercepted by updateInput
// before the textarea sees them; `InsertNewline` is honored by the
// textarea natively after it is wired into KeyMap.InsertNewline.
type commentInputBindings struct {
	Save          key.Binding
	InsertNewline key.Binding
	Cancel        key.Binding
}

// newCommentInputBindings returns the canonical bindings. Help strings
// are written for the help overlay verbatim — render_help.go reads
// them via `Help()` instead of hard-coding labels.
func newCommentInputBindings() commentInputBindings {
	return commentInputBindings{
		Save: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "save comment (new) · save edit (existing)"),
		),
		InsertNewline: key.NewBinding(
			key.WithKeys("shift+enter", "alt+enter", "ctrl+j"),
			key.WithHelp("alt+enter · shift+enter", "insert newline"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

// moveInputBindings is the single source of truth for the modal
// move-input keystrokes. Single-line, so no newline binding —
// `Save` submits the typed bucket key and `Cancel` aborts.
type moveInputBindings struct {
	Save   key.Binding
	Cancel key.Binding
}

func newMoveInputBindings() moveInputBindings {
	return moveInputBindings{
		Save: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit target bucket key"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

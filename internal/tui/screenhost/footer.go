package screenhost

// Footer vocabulary names the key bindings that recur across screens, so a
// screen declares WHICH standard action it offers instead of re-spelling the
// key and the catalog lookup every time. Each constructor pairs one key
// spelling with one label and takes Primary as a parameter, because whether a
// binding is primary is a per-screen layout choice, not a property of the key.
//
// A binding earns a place here only once it recurs across three or more
// screen packages; a binding used by one or two screens stays a literal
// FooterBinding at the call site. These methods live on Frame because Frame
// already resolves catalog text through Text, and a screen already holds a
// Frame wherever it builds its footer.
//
// FooterScroll (the "j/k" key) and FooterScrollPage / FooterPage (the
// "pgup/pgdn" key) are three separate entries, not one parameterized by
// label, because "pgup/pgdn" is labelled two different ways across the
// screens that use it today — see the doc comment on FooterScrollPage.
// Likewise FooterMove, FooterMoveVim and FooterMoveTask share the
// "tui.footer.move" wording under three different keys for three different
// reasons; they are kept distinct rather than merged into one signature.

// FooterCancel is the "esc" binding that discards an in-progress input or
// mode without navigating away from the screen.
func (f Frame) FooterCancel(primary bool) FooterBinding {
	return FooterBinding{Key: "esc", Label: f.Text("tui.footer.cancel"), Primary: primary}
}

// FooterHelp is the "?" binding that opens the help overlay.
func (f Frame) FooterHelp(primary bool) FooterBinding {
	return FooterBinding{Key: "?", Label: f.Text("tui.footer.help"), Primary: primary}
}

// FooterTopBottom is the "g/G" binding that jumps the focused section to its
// first or last item.
func (f Frame) FooterTopBottom(primary bool) FooterBinding {
	return FooterBinding{Key: "g/G", Label: f.Text("tui.footer.top_bottom"), Primary: primary}
}

// FooterRefresh is the "r" binding that reloads the screen's data.
func (f Frame) FooterRefresh(primary bool) FooterBinding {
	return FooterBinding{Key: "r", Label: f.Text("tui.footer.refresh"), Primary: primary}
}

// FooterScroll is the "j/k" binding labelled "scroll": one line or item at a
// time, on a screen whose footer also uses "pgup/pgdn" for the page-sized
// step (see FooterPage).
func (f Frame) FooterScroll(primary bool) FooterBinding {
	return FooterBinding{Key: "j/k", Label: f.Text("tui.footer.scroll"), Primary: primary}
}

// FooterScrollPage is the "pgup/pgdn" binding labelled "scroll" rather than
// "page". It dispatches through the identical page-step action as FooterPage
// — the screens that use it are cursor-list screens (board, graph, logs,
// relationshippicker, settingspicker, table, taskdetail) where a sibling
// screen doing the exact same cursor-list paging (plannetwork, plans' list)
// labels it "page" instead. That split predates this vocabulary and is
// preserved verbatim rather than resolved here, because picking one label is
// a product decision, not a refactor.
func (f Frame) FooterScrollPage(primary bool) FooterBinding {
	return FooterBinding{Key: "pgup/pgdn", Label: f.Text("tui.footer.scroll"), Primary: primary}
}

// FooterPage is the "pgup/pgdn" binding labelled "page": a page-sized step,
// on a screen whose footer also uses "j/k" for the one-line step (see
// FooterScroll). See FooterScrollPage for the sibling screens that bind the
// same key to the word "scroll" instead.
func (f Frame) FooterPage(primary bool) FooterBinding {
	return FooterBinding{Key: "pgup/pgdn", Label: f.Text("tui.footer.page"), Primary: primary}
}

// FooterEdit is the "e" binding that opens the edit form for the focused
// item.
func (f Frame) FooterEdit(primary bool) FooterBinding {
	return FooterBinding{Key: "e", Label: f.Text("tui.footer.edit"), Primary: primary}
}

// FooterOpen is the "enter" binding that opens the focused item.
func (f Frame) FooterOpen(primary bool) FooterBinding {
	return FooterBinding{Key: "enter", Label: f.Text("tui.footer.open"), Primary: primary}
}

// FooterBack is the "esc" binding that leaves the screen for its parent.
// Unlike FooterCancel, it navigates away rather than discarding local state.
func (f Frame) FooterBack(primary bool) FooterBinding {
	return FooterBinding{Key: "esc", Label: f.Text("tui.footer.back"), Primary: primary}
}

// FooterHistoryBack is the "ctrl+o" binding that pops the most recent entry
// off the root's navigation history, on a screen where "esc" is already
// spent on something else.
func (f Frame) FooterHistoryBack(primary bool) FooterBinding {
	return FooterBinding{Key: "ctrl+o", Label: f.Text("tui.footer.back"), Primary: primary}
}

// FooterNew is the "n" binding that opens the create form for a new item.
func (f Frame) FooterNew(primary bool) FooterBinding {
	return FooterBinding{Key: "n", Label: f.Text("tui.footer.new"), Primary: primary}
}

// FooterSave is the "ctrl+s" binding that saves the in-progress form.
func (f Frame) FooterSave(primary bool) FooterBinding {
	return FooterBinding{Key: "ctrl+s", Label: f.Text("tui.footer.save"), Primary: primary}
}

// FooterToggleMarkdown is the "M" binding that switches a rendered body
// between markdown and plain text.
func (f Frame) FooterToggleMarkdown(primary bool) FooterBinding {
	return FooterBinding{Key: "M", Label: f.Text("tui.footer.toggle_markdown"), Primary: primary}
}

// FooterMove is the "up/down" binding that walks a cursor across a list of
// items. See FooterMoveVim for the "j/k" spelling of the same wording, and
// FooterMoveTask for the "m" binding that opens a move action instead of
// walking a cursor.
func (f Frame) FooterMove(primary bool) FooterBinding {
	return FooterBinding{Key: "up/down", Label: f.Text("tui.footer.move"), Primary: primary}
}

// FooterMoveVim is the "j/k" binding that walks a cursor across a list of
// items — the vim-key spelling of FooterMove's "up/down".
func (f Frame) FooterMoveVim(primary bool) FooterBinding {
	return FooterBinding{Key: "j/k", Label: f.Text("tui.footer.move"), Primary: primary}
}

// FooterMoveTask is the "m" binding that opens the move-to-bucket picker for
// the focused task. It shares FooterMove's label because both read as "move"
// in the footer, but it triggers an action rather than walking a cursor.
func (f Frame) FooterMoveTask(primary bool) FooterBinding {
	return FooterBinding{Key: "m", Label: f.Text("tui.footer.move"), Primary: primary}
}

// FooterSubNav is the ",//" binding that cycles the screen's sub-navigation
// strip.
func (f Frame) FooterSubNav(primary bool) FooterBinding {
	return FooterBinding{Key: ",//", Label: f.Text("tui.footer.subs"), Primary: primary}
}

// FooterCloseFocus is the "f/esc" binding that leaves a full-view focus mode.
func (f Frame) FooterCloseFocus(primary bool) FooterBinding {
	return FooterBinding{Key: "f/esc", Label: f.Text("tui.footer.close_focus"), Primary: primary}
}

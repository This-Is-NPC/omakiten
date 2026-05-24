// Package cursorwindow is the canonical cursor + scroll state holder
// for fixed-row TUI surfaces. The package skeleton lives here to make
// the property test red until the real implementation lands.
package cursorwindow

// Model is the placeholder shape — real implementation comes in the
// next commit alongside the resync wire-up.
type Model struct{}

// New returns an empty placeholder; the real implementation arrives
// next commit.
func New(viewportRows int) Model { return Model{} }

func (m Model) Cursor() int                    { return 0 }
func (m Model) Scroll() int                    { return 0 }
func (m Model) ItemCount() int                 { return 0 }
func (m Model) ViewportRows() int              { return 0 }
func (m Model) VisibleRange() (int, int)       { return 0, 0 }
func (m Model) MoveCursor(int) Model           { return m }
func (m Model) JumpFirst() Model               { return m }
func (m Model) JumpLast() Model                { return m }
func (m Model) PageUp() Model                  { return m }
func (m Model) PageDown() Model                { return m }
func (m Model) SetCursor(int) Model            { return m }
func (m Model) WithItemCount(int) Model        { return m }
func (m Model) WithViewport(int) Model         { return m }

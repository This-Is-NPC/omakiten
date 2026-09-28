package settings

import (
	"strings"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionBody is the one section Settings declares. General and Guards share
// the same Screen and the same section id — only the body content differs.
// MinRows is a hard floor (K1): with a single Weight:1 section there is no
// sibling to drop when the HostBox is short.
const sectionBody = screenlayout.ID("settings-body")

func (s Screen) root(kit screenkit.Kit) screengrid.Node {
	section := s.bodySection(kit)
	root := screengrid.Cell(section.Def, section.Body)
	if s.mount != nil {
		s.mount.root, s.mount.rootReady = root, true
	}
	return root
}

func (s Screen) cachedRoot(kit screenkit.Kit) screengrid.Node {
	if s.mount != nil && s.mount.rootReady {
		return s.mount.root
	}
	return s.root(kit)
}

func (s Screen) bodySection(kit screenkit.Kit) screenlayout.Func {
	section := screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionBody, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.bodyBlock(kit, max(0, canvas.Width()-2))
		},
	}
	if s.mount != nil {
		s.mount.section, s.mount.sectionReady = section, true
	}
	return section
}

// bodyBlock exposes every body line as an item so scroll stays line-based, and
// pins the blank spacer + hint as Footer so the grid charges those rows inside
// bodyBox.
func (s Screen) bodyBlock(kit screenkit.Kit, width int) screenlayout.Block {
	return screenlayout.Block{
		Items:  strings.Split(s.body(kit, width), "\n"),
		Footer: s.hintFooter(kit),
		Cursor: screenlayout.NoSelection(),
	}
}

// hintFooter is the chrome under the scrolled body: a blank spacer and the
// hint line. wrapLines folds the hint at the section width, so a bare indented
// line cannot paint past the box View indents into.
func (s Screen) hintFooter(kit screenkit.Kit) []string {
	hintKey := "tui.settings.general_hint"
	if s.id == screenhost.SettingsGuards {
		hintKey = "tui.settings.guards_hint"
	}
	return []string{"", kit.Styles.Hint.Render(kit.T(hintKey))}
}

// bodyBox is shared by the Cell's key/resync path and its single-section paint.
// The framed tables reserve the two-column body indent from the Canvas width,
// preserving the historical content budget without consulting the Kit.
func (s Screen) bodyBox(kit screenkit.Kit) screenlayout.Box {
	host := screenlayout.HostBox(kit)
	return screenlayout.Box{Width: kit.BoxWidth(), Rows: host.Rows}
}

// gridView paints the body through screengrid, the single entry to the body.
//
// A one-Cell root paints exactly what arranging its mounted section painted:
// screengrid's arrangeLeaf rebuilds a screenlayout.Func from the Cell's Spec
// and body and calls the same screenlayout.ArrangeIn with the same kit, the
// same box and the same layout state. The only delta it applies is clearing
// Spec.Column/Spec.Group, and sectionBody declares neither.
//
// cachedRoot is the mounted Node, so the mount cache stays hot across every key
// round trip exactly as the section cache did.
func (s Screen) gridView(kit screenkit.Kit) string {
	return screengrid.Render(kit, s.grid, s.bodyBox(kit), s.cachedRoot(kit)).View
}

func (s Screen) resync(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.Resync(kit, s.bodyBox(kit), s.root(kit))
	return s
}

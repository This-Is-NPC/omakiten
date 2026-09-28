// Package entitylist owns the parameterized settings entity lists.
package entitylist

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/keynav"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

type Kind string

const (
	KindLaws      Kind = "laws"
	KindPersonas  Kind = "personas"
	KindSkills    Kind = "skills"
	KindTemplates Kind = "templates"
	KindTags      Kind = "tags"
)

type Descriptor struct {
	ID   screenhost.ID
	Kind Kind
}

func Laws() Descriptor      { return Descriptor{ID: screenhost.SettingsLaws, Kind: KindLaws} }
func Personas() Descriptor  { return Descriptor{ID: screenhost.SettingsPersonas, Kind: KindPersonas} }
func Skills() Descriptor    { return Descriptor{ID: screenhost.SettingsSkills, Kind: KindSkills} }
func Templates() Descriptor { return Descriptor{ID: screenhost.SettingsTemplates, Kind: KindTemplates} }
func Tags() Descriptor      { return Descriptor{ID: screenhost.SettingsTags, Kind: KindTags} }

func ForID(id screenhost.ID) (Descriptor, bool) {
	for _, descriptor := range []Descriptor{Laws(), Personas(), Skills(), Templates(), Tags()} {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

// cardBoxWidth is the width handed to the box style; lipgloss adds the border
// outside it, so a cell occupies two more columns. cardContentWidth is what is
// left inside the padding. Both mirror the root's entity-card geometry, which is
// what this grid rendered before it painted its own.
const (
	cardBoxWidth     = 26
	cardContentWidth = 24
)

type Item struct {
	Slug, Label string
	Badges      []string
}

// entityListCardCache keeps the cursor-independent card paint. Moving the
// cursor only changes one border; rebuilding every card on every composition
// would charge the whole catalog per key.
type entityListCardCache struct {
	source []Item
	items  []string
}

type Screen struct {
	descriptor Descriptor
	items      []Item
	grid       screengrid.State
	cards      *entityListCardCache
	deleteSlug string
	mergeSlug  string
	loading    bool
	err        error
}

// New is a screen on its entry state: card 0 selected, which is also what
// Spec.SelectFirst makes the arranger resolve on the first frame it composes.
// Seeding it here is what lets a screen the host has BOUND but not yet composed
// answer SelectedSlug — the host reads the selection to build its footer and to
// route a key, and both can happen before any frame. Like every other write
// this screen makes it is a request, re-clamped by the first resolve.
func New(descriptor Descriptor) Screen {
	screen := Screen{descriptor: descriptor, grid: screengrid.NewState(), cards: &entityListCardCache{}}
	return screen.withCursorRequest(0)
}
func (s Screen) ID() screenhost.ID { return s.descriptor.ID }
func (s Screen) Kind() Kind        { return s.descriptor.Kind }

// Cursor is the selected CARD — the arranger's item index for the one section
// this screen declares, and the only cursor it has. The cards-per-line fold is
// stated as Block.PerRow and the arranger keeps the cursor on a card across it,
// so this number does not change when the terminal changes width.
func (s Screen) Cursor() int { return s.grid.Layout().Cursor(sectionGrid) }

// Scroll is the arranger's window offset, exposed for fixture assertions. It is
// a card index like the cursor, and always the first card of a line.
func (s Screen) Scroll() int { return s.grid.Layout().Offset(sectionGrid) }

func (s Screen) SelectedSlug() string {
	cursor := s.Cursor()
	if cursor < 0 || cursor >= len(s.items) {
		return ""
	}
	return s.items[cursor].Slug
}

func (s Screen) Bind(items []Item, err error) Screen {
	changed := !sameItems(s.items, items)
	s.items = append(s.items[:0], items...)
	s.err, s.loading = err, false
	if !changed {
		return s
	}
	// A refresh is the one moment this screen writes a cursor, and it has no
	// frame to resolve against — the host binds outside the render. The write
	// goes through screengrid.State.WithCursor, which is a REQUEST: it drops the
	// measurement taken against the items that just went away, and the next
	// Resync, HandleKey or Arrange clamps whatever it asked for. The bound here
	// is only so a host reading the selection before that frame arrives is not
	// handed an index the shorter list no longer has.
	return s.withCursorRequest(min(s.Cursor(), len(s.items)-1))
}

// withCursorRequest seeds the grid's cursor without discarding the frame memo
// the last composition recorded — the reason screengrid.State.WithCursor exists
// (f92672b4). WithLayout(Layout().WithCursor(...)) would throw that memo away
// for a write no part of it witnesses.
func (s Screen) withCursorRequest(index int) Screen {
	s.grid = s.grid.WithCursor(sectionGrid, index)
	return s
}

func sameItems(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Slug != b[i].Slug || a[i].Label != b[i].Label || len(a[i].Badges) != len(b[i].Badges) {
			return false
		}
		for j := range a[i].Badges {
			if a[i].Badges[j] != b[i].Badges[j] {
				return false
			}
		}
	}
	return true
}

func cloneItems(items []Item) []Item {
	clone := make([]Item, len(items))
	for i := range items {
		clone[i] = items[i]
		clone[i].Badges = append([]string(nil), items[i].Badges...)
	}
	return clone
}

func (s Screen) Loading() Screen {
	s.loading, s.err = true, nil
	return s
}

func (s Screen) CancelDelete() Screen {
	s.deleteSlug = ""
	return s
}

func (s Screen) CancelMerge() Screen {
	s.mergeSlug = ""
	return s
}

func (s Screen) CancelArmed() Screen {
	s.deleteSlug = ""
	s.mergeSlug = ""
	return s
}

func (s Screen) MergeSlug() string { return s.mergeSlug }

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	slug := s.SelectedSlug()
	var outcome screenhost.Outcome
	var handled bool
	s, outcome, handled = s.primaryAction(key, slug)
	if handled {
		return outcome
	}
	s, outcome, handled = s.deleteAction(key, slug)
	if handled {
		return outcome
	}
	s, outcome, handled = s.mergeAction(key, slug)
	if handled {
		return outcome
	}
	s, outcome, handled = s.secondaryAction(key, slug)
	if handled {
		return outcome
	}
	// Motion is the grid's. Every key this screen advertises for it —
	// j/k, the page pair, g/G — comes from screenlayout.StandardBindings, and
	// Block.PerRow is what makes j step one CARD across a line of them, so
	// there is no motion table here to fall out of step with the footer.
	kit := frame.Kit()
	if next, handled := s.grid.HandleKey(kit, s.contentBox(kit), key.String(), s.root(frame)); handled {
		// Moving the selection disarms a pending delete, exactly as it did when
		// this screen owned the motion. A pending MERGE survives: its second
		// press is on a different tag, so it has to be reachable.
		s.deleteSlug = ""
		s.grid = next
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) primaryAction(key tea.KeyMsg, slug string) (Screen, screenhost.Outcome, bool) {
	if key.String() != "enter" && key.String() != "n" && key.String() != "e" {
		return s, screenhost.Outcome{}, false
	}
	s.deleteSlug = ""
	if s.descriptor.Kind == KindTags {
		return s, screenhost.Outcome{}, false
	}
	switch key.String() {
	case "enter":
		return s, entityAction(s, screenhost.ActionOpenEntity, slug), true
	case "n":
		if s.descriptor.Kind == KindTemplates {
			return s, entityAction(s, screenhost.ActionTemplateCreateHint, slug), true
		}
		return s, entityAction(s, screenhost.ActionCreateEntity, ""), true
	default:
		return s, entityAction(s, screenhost.ActionEditEntity, slug), true
	}
}

func (s Screen) deleteAction(key tea.KeyMsg, slug string) (Screen, screenhost.Outcome, bool) {
	if key.String() != "d" && key.String() != "D" {
		return s, screenhost.Outcome{}, false
	}
	if key.String() == "D" {
		s.deleteSlug, s.mergeSlug = "", ""
		if s.descriptor.Kind == KindTags {
			return s, action(s, screenhost.ActionDeleteOrphanTags), true
		}
		return s, screenhost.Outcome{}, false
	}
	if slug == "" {
		return s, screenhost.Stay(s, nil), true
	}
	if s.descriptor.Kind == KindTemplates {
		s.deleteSlug, s.mergeSlug = "", ""
		return s, entityAction(s, screenhost.ActionTemplateDeleteHint, slug), true
	}
	s.mergeSlug = ""
	if s.deleteSlug == slug {
		s.deleteSlug = ""
		if s.descriptor.Kind == KindTags {
			return s, entityAction(s, screenhost.ActionDeleteTag, slug), true
		}
		return s, entityAction(s, screenhost.ActionDeleteEntity, slug), true
	}
	s.deleteSlug = slug
	if s.descriptor.Kind == KindTags {
		return s, entityAction(s, screenhost.ActionPrepareTagDelete, slug), true
	}
	return s, entityAction(s, screenhost.ActionPrepareEntityDelete, slug), true
}

func (s Screen) mergeAction(key tea.KeyMsg, slug string) (Screen, screenhost.Outcome, bool) {
	if key.String() != "m" || s.descriptor.Kind != KindTags {
		return s, screenhost.Outcome{}, false
	}
	if slug == "" {
		return s, screenhost.Stay(s, nil), true
	}
	s.deleteSlug = ""
	if s.mergeSlug == "" {
		s.mergeSlug = slug
		return s, entityAction(s, screenhost.ActionPrepareTagMerge, slug), true
	}
	if s.mergeSlug == slug {
		return s, entityAction(s, screenhost.ActionPrepareTagMerge, slug), true
	}
	source := s.mergeSlug
	s.mergeSlug = ""
	return s, screenhost.MergeTagsAction(s, source, slug, nil), true
}

func (s Screen) secondaryAction(key tea.KeyMsg, slug string) (Screen, screenhost.Outcome, bool) {
	if key.String() == "esc" {
		if s.deleteSlug == "" && s.mergeSlug == "" {
			return s, screenhost.Stay(s, nil), true
		}
		s.deleteSlug, s.mergeSlug = "", ""
		return s, action(s, screenhost.ActionCancelEntityDelete), true
	}
	if key.String() != "p" && key.String() != "a" && key.String() != "t" && key.String() != "c" && key.String() != "r" {
		return s, screenhost.Outcome{}, false
	}
	s.deleteSlug = ""
	switch key.String() {
	case "p":
		if s.descriptor.Kind == KindPersonas {
			return s, entityAction(s, screenhost.ActionOpenPersonaSkills, slug), true
		}
	case "a":
		if s.descriptor.Kind == KindTemplates {
			return s, entityAction(s, screenhost.ActionOpenTemplateDefault, slug), true
		}
	case "t":
		return s, action(s, screenhost.ActionOpenThemePicker), true
	case "c":
		return s, action(s, screenhost.ActionOpenConfigPicker), true
	case "r":
		return s, screenhost.Reload(s, nil), true
	}
	return s, screenhost.Outcome{}, false
}

func entityAction(s Screen, kind screenhost.ActionKind, slug string) screenhost.Outcome {
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: kind, EntityKind: string(s.descriptor.Kind), Value: slug}}
}

func action(s Screen, kind screenhost.ActionKind) screenhost.Outcome {
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: kind}}
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.deleteSlug, s.mergeSlug = "", ""
		s.grid = screengrid.NewState()
		if s.cards != nil {
			s.cards.source, s.cards.items = nil, nil
		}
	}
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		// A fresh grid state carries no cursor; Spec.SelectFirst is what puts
		// the entry state on card 0, resolved and persisted by this Resync.
		s = s.resyncGrid(frame)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "up", "k", "down", "j", "pgup", "ctrl+u", "pgdown", "pgdn", "ctrl+d", "home", "g", "end", "G", "enter", "n", "e", "d", "D", "m", "esc", "p", "a", "t", "c", "r":
		return true
	}
	return false
}

func (s Screen) OwnsFooter() bool        { return true }
func (s Screen) ResetOnNavigation() bool { return true }

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if s.deleteSlug != "" {
		return []screenhost.FooterBinding{{Key: "d", Label: frame.Text("tui.footer.confirm_delete"), Primary: true}, frame.FooterCancel(false)}
	}
	if s.mergeSlug != "" {
		return []screenhost.FooterBinding{{Key: "m", Label: frame.Text("tui.footer.confirm_merge"), Primary: true}, frame.FooterCancel(false)}
	}
	bindings := []screenhost.FooterBinding{
		frame.FooterOpen(true),
		frame.FooterNew(true),
		frame.FooterEdit(true),
		{Key: "d", Label: frame.Text("tui.footer.arm_delete")},
	}
	if s.descriptor.Kind == KindPersonas {
		bindings = append(bindings, screenhost.FooterBinding{Key: "p", Label: frame.Text("tui.footer.skills_persona")})
	}
	if s.descriptor.Kind == KindTemplates {
		bindings = append(bindings, screenhost.FooterBinding{Key: "a", Label: frame.Text("tui.footer.set_default")})
	}
	if s.descriptor.Kind == KindTags {
		bindings = []screenhost.FooterBinding{
			{Key: "d", Label: frame.Text("tui.footer.arm_delete"), Primary: true},
			{Key: "m", Label: frame.Text("tui.footer.arm_merge"), Primary: true},
			{Key: "D", Label: frame.Text("tui.footer.delete_orphans")},
		}
	}
	return append(bindings,
		screenhost.FooterBinding{Key: "up/down", Label: frame.Text("tui.footer.select")},
		screenhost.FooterBinding{Key: "t", Label: frame.Text("tui.footer.theme")},
		screenhost.FooterBinding{Key: "c", Label: frame.Text("tui.footer.config")},
		screenhost.FooterBinding{Key: keynav.Default.Tops.Primary(), Label: frame.Text("tui.footer.tabs")},
		screenhost.FooterBinding{Key: keynav.Default.Zones.Primary(), Label: frame.Text("tui.footer.zones")},
		frame.FooterSubNav(false),
		frame.FooterHelp(false),
	)
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "settings_entity", Title: frame.Text("tui.help.settings_entity.title"), Bindings: []screenhost.HelpBinding{
		{Key: ", · /", Description: frame.Text("tui.help.settings_entity.prev_next_sub")},
		{Key: "↑ ↓ · j k", Description: frame.Text("tui.help.settings_entity.select")},
		{Key: "pgup · pgdn · ctrl+u · ctrl+d", Description: frame.Text("tui.help.settings_entity.scroll_halfpage")},
		{Key: "g · G", Description: frame.Text("tui.help.settings_entity.first_last")},
		{Key: "enter", Description: frame.Text("tui.help.settings_entity.open_detail")},
		{Key: "n", Description: frame.Text("tui.help.settings_entity.new_entity")},
		{Key: "e", Description: frame.Text("tui.help.settings_entity.edit_in_editor")},
		{Key: "d · d", Description: frame.Text("tui.help.settings_entity.arm_delete")},
		{Key: "p", Description: frame.Text("tui.help.settings_entity.skill_picker")},
		{Key: "t · c", Description: frame.Text("tui.help.settings_entity.theme_or_config_picker")},
	}}}
}

func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	id := screenstate.For(string(s.descriptor.Kind))
	return []screenstate.State{
		id.Failed(s.err, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.generic")),
		id.Vazio(len(s.items) == 0, kit.T("tui.empty.no_items"), ""),
	}
}

func (s Screen) root(frame screenhost.Frame) screengrid.Node {
	section := s.gridSection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...); ok {
		return kit.Panel(body)
	}
	// Render fills contentBox (kicker charged as Header) without a leading blank.
	// View then frames the whole arranged body so PerRow cards sit inside
	// │…│ with a joining ├─┤ — wrapping each card in the Block would box every
	// cell, and Frame as Block header/footer left the cards outside the sides.
	_, inner := s.columnWidths(kit)
	body := screengrid.Render(kit, s.grid, s.contentBox(kit), s.root(frame)).View
	return "\n" + screenkit.Indent(s.frameBody(kit, body, inner), 2)
}

func (s Screen) frameBody(kit screenkit.Kit, body string, inner int) string {
	lines := strings.Split(body, "\n")
	kicker := ""
	rest := lines
	if len(lines) > 0 {
		kicker, rest = lines[0], lines[1:]
	}
	block := framed.Box(kit.Styles.Border, max(1, inner)+panel.Borders, kicker, rest)
	rows := append(append(append([]string{}, block.Header...), block.Items...), block.Footer...)
	return strings.Join(rows, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.NavigationResetter = Screen{}

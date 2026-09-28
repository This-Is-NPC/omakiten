// Package settings owns the read-only General and Guards settings screens.
package settings

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/keynav"
	"omakiten/internal/settingsprojection"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

type Runtime struct {
	Version, Scope, ConfigPath, DBPath string
}

type Payload struct {
	Runtime   Runtime
	Workflow  domain.Workflow
	ThemeKey  string
	Languages config.LanguageSettings
	Snapshot  *config.Snapshot
}

type Screen struct {
	id      screenhost.ID
	payload Payload
	grid    screengrid.State
	mount   *settingsBodyCache
	err     error
}

type settingsBodyCache struct {
	section      screenlayout.Func
	sectionReady bool
	root         screengrid.Node
	rootReady    bool
}

func NewGeneral() Screen {
	return Screen{id: screenhost.SettingsGeneral, grid: screengrid.NewState(), mount: &settingsBodyCache{}}
}
func NewGuards() Screen {
	return Screen{id: screenhost.SettingsGuards, grid: screengrid.NewState(), mount: &settingsBodyCache{}}
}
func (s Screen) ID() screenhost.ID { return s.id }
func (s Screen) Scroll() int       { return s.grid.Layout().Offset(sectionBody) }
func (s Screen) Bind(payload Payload, err error) Screen {
	s.payload, s.err, s.mount = payload, err, &settingsBodyCache{}
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	switch key.String() {
	case "t":
		return action(s, screenhost.ActionOpenThemePicker)
	case "c":
		return action(s, screenhost.ActionOpenConfigPicker)
	case "s":
		return action(s, screenhost.ActionOpenSubtaskKitPicker)
	case "e":
		return action(s, screenhost.ActionOpenConfigEditor)
	case "r":
		return screenhost.Reload(s, nil)
	}
	kit := frame.Kit()
	if next, handled := s.grid.HandleKey(kit, s.bodyBox(kit), key.String(), s.cachedRoot(kit)); handled {
		s.grid = next
	}
	return screenhost.Stay(s, nil)
}

func action(s Screen, kind screenhost.ActionKind) screenhost.Outcome {
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: kind}}
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.grid = screengrid.NewState()
	}
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s = s.resync(frame)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "t", "c", "s", "e", "r", "down", "j", "up", "k", "pgdown", "pgdn", "ctrl+d", "pgup", "ctrl+u", "home", "g", "end", "G":
		return true
	}
	return false
}

func (s Screen) OwnsFooter() bool        { return true }
func (s Screen) ResetOnNavigation() bool { return true }
func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{
		{Key: "t", Label: frame.Text("tui.footer.theme"), Primary: true},
		{Key: "c", Label: frame.Text("tui.footer.config"), Primary: true},
		{Key: "s", Label: frame.Text("tui.footer.subtask_kit"), Primary: true},
		frame.FooterEdit(true),
		frame.FooterScroll(false),
		frame.FooterPage(false),
		frame.FooterTopBottom(false),
		{Key: keynav.Default.Tops.Primary(), Label: frame.Text("tui.footer.tabs")},
		{Key: keynav.Default.Zones.Primary(), Label: frame.Text("tui.footer.zones")},
		frame.FooterSubNav(false),
		frame.FooterHistoryBack(false),
		frame.FooterHelp(false),
	}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	id := "settings_general"
	if s.id == screenhost.SettingsGuards {
		id = "settings_guards"
	}
	return []screenhost.HelpGroup{{ID: id, Title: frame.Text("tui.help.settings_general.title"), Bindings: []screenhost.HelpBinding{
		{Key: ", · /", Description: frame.Text("tui.help.settings_general.prev_next_sub")},
		{Key: "t", Description: frame.Text("tui.help.settings_general.theme_picker")},
		{Key: "c", Description: frame.Text("tui.help.settings_general.config_picker")},
		{Key: "e", Description: frame.Text("tui.help.settings_general.edit_config")},
		{Key: "r", Description: frame.Text("tui.help.settings_general.refresh")},
	}}}
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...); ok {
		return kit.Panel(body)
	}
	// The Cell body owns the explicit leading blank row before the standard
	// two-column body indent.
	body := "\n" + s.gridView(kit)
	return screenkit.Indent(body, 2)
}

func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	id := screenstate.For(frame.Kit().T("tui.help.settings_general.title"))
	return []screenstate.State{id.Failed(s.err, frame.Kit().T("tui.stat.error_badge"))}
}

func (s Screen) body(kit screenkit.Kit, width int) string {
	if s.id == screenhost.SettingsGuards {
		return s.guardsBody(kit, width)
	}
	return s.generalBody(kit, width)
}

func (s Screen) generalBody(kit screenkit.Kit, width int) string {
	dash := func(value string) string {
		value = screenkit.Sanitize(value)
		if value == "" {
			return kit.Styles.Hint.Render("—")
		}
		return value
	}
	buckets := make([]string, len(s.payload.Workflow.Buckets))
	for i, bucket := range s.payload.Workflow.Buckets {
		buckets[i] = bucket.Key
	}
	sort.Strings(buckets)
	tables := [][][]gridtable.Cell{
		focusedRows(kit, kit.T("tui.kicker.runtime"),
			[2]string{kit.T("tui.settings.runtime.version"), dash(s.payload.Runtime.Version)},
			[2]string{kit.T("tui.settings.runtime.scope"), dash(s.payload.Runtime.Scope)},
			[2]string{kit.T("tui.settings.runtime.config"), dash(s.payload.Runtime.ConfigPath)},
			[2]string{kit.T("tui.settings.runtime.database"), dash(s.payload.Runtime.DBPath)},
			[2]string{kit.T("tui.settings.runtime.lang_cli"), dash(s.payload.Languages.CLI)},
			[2]string{kit.T("tui.settings.runtime.lang_tui"), dash(s.payload.Languages.TUI)},
			[2]string{kit.T("tui.settings.runtime.lang_agent_output"), dash(s.payload.Languages.AgentOutput)}),
		focusedRows(kit, kit.T("tui.kicker.project"),
			[2]string{kit.T("tui.settings.project.workflow"), dash(s.payload.Workflow.Key)},
			[2]string{kit.T("tui.settings.project.buckets"), dash(strings.Join(buckets, ", "))},
			[2]string{kit.T("tui.settings.project.theme"), dash(s.payload.ThemeKey)}),
	}
	tables = append(tables, effectiveTables(kit, s.payload.Snapshot)...)
	return gridtable.Summaries(width, kit.Styles.Border, gridtable.Options{LabelWidth: 24, ValueWidth: 46, Auto: true}, tables...)
}

func effectiveTables(kit screenkit.Kit, snapshot *config.Snapshot) [][][]gridtable.Cell {
	sections := settingsprojection.EffectiveSections(snapshot)
	var tables [][][]gridtable.Cell
	for _, section := range sections {
		fields := make([][2]string, 0, len(section.Tuples))
		for _, tuple := range section.Tuples {
			key := screenkit.Sanitize(tuple.Key)
			if key == "" {
				key = section.Name
			}
			fields = append(fields, [2]string{key, truncate(tuple.Value, 80)})
		}
		if len(fields) > 0 {
			tables = append(tables, focusedRows(kit, kit.T("tui.settings.effective.section."+section.Name), fields...))
		}
	}
	return tables
}

// focusedRows is [gridtable.Rows] with its title row repainted through the
// focused section marker instead of the plain structural one. Settings
// declares exactly one screengrid zone (see layout.go's sectionBody) — there
// is no sibling zone for it to lose focus to — so every top-level table it
// stacks is that zone's focused section the same way Stats, Logs, Plans and
// Table paint their own single zone: the marker is always on, not toggled by
// a computed bool. The field rows [gridtable.Rows] builds alongside the
// title stay under kit.Styles.Kicker; those are column labels, not section
// titles, the same distinction Guards' matrix draws (see guards.go).
func focusedRows(kit screenkit.Kit, title string, fields ...[2]string) [][]gridtable.Cell {
	rows := gridtable.Rows(kit.Styles.Kicker, title, fields...)
	rows[0] = []gridtable.Cell{gridtable.Styled(kit.Styles.FocusKicker(title))}
	return rows
}

func truncate(value string, limit int) string {
	value = screenkit.Sanitize(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func (s Screen) guardsBody(kit screenkit.Kit, width int) string {
	input := func(snapshot *config.Snapshot, workflow domain.Workflow, label string) guardInput {
		return guardInput{Snapshot: snapshot, Workflow: workflow, Label: label,
			From: kit.T("tui.settings.guards.header_from"), To: kit.T("tui.settings.guards.header_to"),
			Disallowed: kit.T("tui.settings.guards.sentinel_disallowed"), Empty: kit.T("tui.settings.guards.sentinel_empty")}
	}
	root := s.payload.Snapshot
	if root == nil {
		return renderGuardMatrix(kit, width, input(nil, s.payload.Workflow, kit.T("tui.settings.guards_tab")))
	}
	sub, hasSub := root.SubtaskKit()
	if !hasSub || settingsprojection.GuardMatricesEqual(root, sub) {
		return renderGuardMatrix(kit, width, input(root, s.payload.Workflow, kit.T("tui.settings.guards_tab")))
	}
	return renderGuardMatrix(kit, width, input(root, root.Workflow(), kit.T("tui.settings.guards.kicker_root"))) + "\n\n" +
		renderGuardMatrix(kit, width, input(sub, sub.Workflow(), kit.T("tui.settings.guards.kicker_subtask")))
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.NavigationResetter = Screen{}

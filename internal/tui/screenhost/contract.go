package screenhost

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screenkit"
)

// ID is the stable identity of an addressable TUI screen.
type ID string

// Stable screen IDs double as the canonical palette route where a screen is
// palette-addressable. They are independent of display labels and ordering.
const (
	Home              ID = "home"
	TasksBoard        ID = "tasks.board"
	TasksTable        ID = "tasks.table"
	TasksGraph        ID = "tasks.graph"
	TasksPlans        ID = "tasks.plans"
	PlanGoal          ID = "plans.goal"
	PlanNetwork       ID = "plans.network"
	Project           ID = "project"
	ProjectForm       ID = "project.form"
	ProjectResume     ID = "project.resume"
	ProjectKnowledge  ID = "project.knowledge"
	StatsGeneral      ID = "stats.general"
	StatsLogs         ID = "stats.logs"
	StatsInsights     ID = "stats.insights"
	StudioWorkflow    ID = "studio.workflow"
	StudioCommands    ID = "studio.commands"
	StudioPersonas    ID = "studio.personas"
	StudioHooks       ID = "studio.hooks"
	SettingsGeneral   ID = "settings.general"
	SettingsLaws      ID = "settings.laws"
	SettingsPersonas  ID = "settings.personas"
	SettingsSkills    ID = "settings.skills"
	SettingsTemplates ID = "settings.templates"
	SettingsTags      ID = "settings.tags"
	SettingsGuards    ID = "settings.guards"
	EntityDetail      ID = "entity.detail"
	ThemePicker       ID = "settings.theme-picker"
	ConfigPicker      ID = "settings.config-picker"
	SubtaskKitPicker  ID = "settings.subtask-kit-picker"
	PersonaSkills     ID = "settings.persona-skills"
	TemplateDefault   ID = "settings.template-default"
	TaskForm          ID = "tasks.form"
	TaskDetail        ID = "tasks.detail"
	TaskDescription   ID = "tasks.description"
	CommentDetail     ID = "comments.detail"
)

// TopID is the stable identity of a top-level navigation zone.
type TopID string

const (
	TopHome     TopID = "home"
	TopTasks    TopID = "tasks"
	TopStats    TopID = "stats"
	TopStudio   TopID = "studio"
	TopSettings TopID = "settings"
)

// FrameOptions is copied into a Frame at the root update/view boundary.
type FrameOptions struct {
	Width       int
	Height      int
	ProjectID   int64
	ProjectSlug string
	Status      string
	Focused     bool
	// ChromeRows is how many terminal rows the host chrome occupies ABOVE the
	// screen body — the screen header (nav + sub strip) plus the status badge
	// when one is showing. A screen cannot measure it, because the chrome is
	// host-owned and renders outside the screen boundary, so the host supplies
	// it and Frame.Kit hands it to the viewport-budget helpers.
	ChromeRows int
	// Styles is the theme projection the screen may paint with, Text
	// resolves i18n catalog keys, and Markdown carries the four colour
	// tokens the markdown renderer needs. All three ride on the frame rather
	// than being captured at construction so a theme swap or a language
	// change lands on the very next frame without the screen holding host state.
	Styles   screenkit.Styles
	Markdown screenkit.MarkdownTokens
	Text     func(string) string
}

// Frame is an immutable snapshot of host-owned context. Its fields are private
// so screens can observe root state without retaining or mutating the root
// Model; a later root update supplies a fresh frame.
type Frame struct {
	width       int
	height      int
	projectID   int64
	projectSlug string
	status      string
	focused     bool
	chromeRows  int
	styles      screenkit.Styles
	markdown    screenkit.MarkdownTokens
	text        func(string) string
}

// NewFrame copies host context into an immutable screen frame.
func NewFrame(options FrameOptions) Frame {
	return Frame{
		width:       options.Width,
		height:      options.Height,
		projectID:   options.ProjectID,
		projectSlug: options.ProjectSlug,
		status:      options.Status,
		focused:     options.Focused,
		chromeRows:  options.ChromeRows,
		styles:      options.Styles,
		markdown:    options.Markdown,
		text:        options.Text,
	}
}

func (f Frame) Width() int          { return f.width }
func (f Frame) Height() int         { return f.height }
func (f Frame) ProjectID() int64    { return f.projectID }
func (f Frame) ProjectSlug() string { return f.projectSlug }
func (f Frame) Status() string      { return f.status }
func (f Frame) Focused() bool       { return f.focused }
func (f Frame) ChromeRows() int     { return f.chromeRows }

// Kit is the rendering toolkit for this frame: the theme projection, the
// catalog resolver and the live geometry a screen needs to lay its body out.
// Assembled on demand from the frame's own fields so geometry has exactly one
// source of truth.
func (f Frame) Kit() screenkit.Kit {
	return screenkit.Kit{
		Styles:     f.styles,
		Markdown:   f.markdown,
		Text:       f.text,
		Width:      f.width,
		Height:     f.height,
		ChromeRows: f.chromeRows,
	}
}

// Text resolves an i18n catalog key against the host catalog, degrading to the
// key itself when no resolver is wired.
func (f Frame) Text(key string) string { return f.Kit().T(key) }

// Host is the narrow root capability passed to a screen factory. Application
// services belong in factory closures; the host exposes only immutable frame
// context and root-owned translation.
type Host interface {
	Frame() Frame
	Text(key string) string
}

// Factory constructs one screen against the host contract. It deliberately
// cannot accept *tui.Model, which keeps extracted screen packages pointed away
// from the root package.
type Factory func(Host) Screen

// FooterBinding is a semantic footer action. The root remains responsible for
// styling and laying bindings out.
type FooterBinding struct {
	Key     string
	Label   string
	Primary bool
}

// HelpBinding is one key-description row in a help group.
type HelpBinding struct {
	Key         string
	Description string
}

// HelpGroup is a stable help section supplied by a screen.
type HelpGroup struct {
	ID       string
	Title    string
	Bindings []HelpBinding
}

// LifecycleEvent reports host-managed screen lifecycle changes.
type LifecycleEvent uint8

const (
	LifecycleEnter LifecycleEvent = iota + 1
	LifecycleLeave
	LifecycleFocus
	LifecycleBlur
	LifecycleResize
)

// ActionKind identifies a semantic request that the root applies after a
// screen update. Screens report intent instead of mutating navigation, status,
// reload generation guards, or process lifecycle directly.
type ActionKind uint8

const (
	ActionNone ActionKind = iota
	ActionNavigate
	ActionBack
	ActionReload
	ActionSetStatus
	ActionOpenTask
	ActionMoveTask
	ActionCreateTask
	ActionEditTask
	ActionMoveTaskToBucket
	ActionOpenPlanGoal
	ActionOpenPlanNetwork
	ActionSavePlanGoal
	ActionSetTaskAssignee
	ActionSelectProject
	ActionCreateProject
	ActionEditProject
	ActionPrepareProjectDelete
	ActionDeleteProject
	ActionOpenProjectForm
	ActionOpenProjectComment
	ActionOpenThemePicker
	ActionOpenConfigPicker
	ActionOpenSubtaskKitPicker
	ActionOpenConfigEditor
	ActionOpenEntity
	ActionCreateEntity
	ActionEditEntity
	ActionPrepareEntityDelete
	ActionDeleteEntity
	ActionCancelEntityDelete
	ActionOpenPersonaSkills
	ActionOpenTemplateDefault
	ActionApplySettingsPicker
	ActionSelectRelationship
	ActionSavePersonaSkills
	ActionSelectTemplateDefault
	ActionCreateRelationship
	ActionCancelRelationshipPicker
	ActionSaveTaskForm
	ActionCancelTaskForm
	ActionLookupTaskParent
	ActionOpenTaskBlockers
	ActionCloseTaskDetail
	ActionRefreshTaskDetail
	ActionCreateSubtask
	ActionOpenNestedTask
	ActionOpenTaskComment
	ActionAddTaskComment
	ActionMoveTaskFromDetail
	ActionSaveTaskBlockers
	ActionDeleteTaskFromDetail
	ActionOpenTaskDescription
	ActionCompleteSubtask
	ActionToggleTaskMarkdown
	ActionSaveComment
	ActionDeleteComment
	ActionTemplateCreateHint
	ActionTemplateDeleteHint
	ActionPrepareTagDelete
	ActionDeleteTag
	ActionDeleteOrphanTags
	ActionPrepareTagMerge
	ActionMergeTags
	ActionArchiveTaskFromDetail
	ActionUnarchiveTaskFromDetail
	ActionOpenProjectResume
	ActionOpenProjectKnowledge
	ActionQuit
)

// Action is the root-facing semantic portion of an Outcome.
type Action struct {
	Kind            ActionKind
	Target          ID
	Status          string
	TaskID          int64
	BucketKey       string
	PlanSlug        string
	PlanID          int64
	Value           string
	SourceValue     string
	EntityKind      string
	ProjectID       int64
	CommentID       int64
	Generation      uint64
	TaskFormMode    string
	TaskTitle       string
	TaskDescription string
	TaskPriority    string
	TaskTagsCSV     string
	TaskParent      string
	TaskIDs         []int64
	DeleteCounters  domain.ProjectDeleteCounters
}

// Outcome is the complete result of an update or lifecycle event.
type Outcome struct {
	Screen  Screen
	Action  Action
	Command tea.Cmd
}

func Stay(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Command: command}
}

func Navigate(screen Screen, target ID, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionNavigate, Target: target}, Command: command}
}

func Back(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionBack}, Command: command}
}

func Reload(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionReload}, Command: command}
}

func SetStatus(screen Screen, status string, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionSetStatus, Status: status}, Command: command}
}

// OpenTask asks the host to open task detail without exposing root detail state
// to the originating screen.
func OpenTask(screen Screen, taskID int64, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionOpenTask, TaskID: taskID}, Command: command}
}

// MoveTask asks the host to open its task-move input for taskID.
func MoveTask(screen Screen, taskID int64, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionMoveTask, TaskID: taskID}, Command: command}
}

func CreateTask(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionCreateTask}, Command: command}
}

func EditTask(screen Screen, taskID int64, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionEditTask, TaskID: taskID}, Command: command}
}

func MoveTaskToBucket(screen Screen, taskID int64, bucketKey string, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionMoveTaskToBucket, TaskID: taskID, BucketKey: bucketKey}, Command: command}
}

func OpenPlanGoal(screen Screen, slug string, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionOpenPlanGoal, PlanSlug: slug}, Command: command}
}

func OpenPlanNetwork(screen Screen, slug string, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionOpenPlanNetwork, PlanSlug: slug}, Command: command}
}

func SavePlanGoal(screen Screen, planID int64, slug, value string, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionSavePlanGoal, PlanID: planID, PlanSlug: slug, Value: value}, Command: command}
}

func SetTaskAssignee(screen Screen, taskID int64, slug, value string, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionSetTaskAssignee, TaskID: taskID, PlanSlug: slug, Value: value}, Command: command}
}

func SelectProject(screen Screen, projectID int64, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionSelectProject, ProjectID: projectID}, Command: command}
}

func CreateProject(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionCreateProject}, Command: command}
}

func ArchiveTaskFromDetail(screen Screen, taskID int64, generation uint64) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionArchiveTaskFromDetail, TaskID: taskID, Generation: generation}}
}

func UnarchiveTaskFromDetail(screen Screen, taskID int64, generation uint64) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionUnarchiveTaskFromDetail, TaskID: taskID, Generation: generation}}
}

func EditProject(screen Screen, projectID int64, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionEditProject, ProjectID: projectID}, Command: command}
}

func PrepareProjectDelete(screen Screen, projectID int64, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionPrepareProjectDelete, ProjectID: projectID}, Command: command}
}

func DeleteProject(screen Screen, projectID int64, counters domain.ProjectDeleteCounters, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionDeleteProject, ProjectID: projectID, DeleteCounters: counters}, Command: command}
}

func OpenProjectForm(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionOpenProjectForm}, Command: command}
}

func OpenProjectComment(screen Screen, commentID int64, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionOpenProjectComment, CommentID: commentID}, Command: command}
}

func OpenProjectResume(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionOpenProjectResume}, Command: command}
}

func OpenProjectKnowledge(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionOpenProjectKnowledge}, Command: command}
}

func MergeTagsAction(screen Screen, sourceSlug, targetSlug string, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionMergeTags, SourceValue: sourceSlug, Value: targetSlug}, Command: command}
}

func Quit(screen Screen, command tea.Cmd) Outcome {
	return Outcome{Screen: screen, Action: Action{Kind: ActionQuit}, Command: command}
}

// Screen is the extraction boundary for a TUI screen. Resize, focus, and
// enter/leave behavior flow through Lifecycle; global overlays and process
// lifecycle remain root-owned.
type Screen interface {
	ID() ID
	Update(Frame, tea.Msg) Outcome
	View(Frame) string
	Footer(Frame) []FooterBinding
	Help(Frame) []HelpGroup
	Lifecycle(Frame, LifecycleEvent) Outcome
}

// InteractionBlocker is an optional screen capability used by the host to
// suppress background refresh and global input overlays during a local mode.
type InteractionBlocker interface {
	BlocksHostInput() bool
}

// HelpBlocker lets an input mode reserve `?` as text instead of allowing the
// host's global help overlay to intercept it.
type HelpBlocker interface {
	BlocksHelp() bool
}

// FooterOwner is an optional capability for screens whose established footer
// ordering interleaves host navigation with screen-local bindings.
type FooterOwner interface {
	OwnsFooter() bool
}

// KeyOwner lets a screen claim local keys before the host's shared shortcuts.
//
// A screen resolves its own vocabulary before delegating a key to a layout or
// grid. Delegation is a decline, not a claim: the arranger must return
// handled=false for keys outside its vocabulary (including `tab` when its
// focus ring has fewer than two members), so the screen or host can continue
// routing them. This order keeps screen-owned `f`/`tab` actions reachable even
// when a nested grid recognizes the same spelling.
type KeyOwner interface {
	OwnsKey(tea.KeyMsg) bool
}

// NavigationResetter opts a base route into LifecycleEnter whenever host
// navigation selects it. Modal routes already receive enter through pushScreen.
type NavigationResetter interface {
	ResetOnNavigation() bool
}

// TaskSelector exposes semantic selection without leaking a screen's cursor or
// projection into root selectors.
type TaskSelector interface {
	SelectedTaskID() (int64, bool)
}

// ReloadPolicy identifies the root-owned data projection a screen needs.
type ReloadPolicy uint8

const (
	ReloadManual ReloadPolicy = iota
	ReloadHome
	ReloadBundle
	ReloadTaskActivity
	ReloadPlan
	ReloadStats
	ReloadLogs
	ReloadInsights
)

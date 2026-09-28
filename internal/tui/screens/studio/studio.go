// Package studio owns the Studio workbench screens, their shared draft, and
// all mode-specific update and rendering behavior.
package studio

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

// BundleEditor is the on-disk bundle mutation surface Studio needs.
// BundleEditor satisfies it; the screen must not import internal/app (D21).
type BundleEditor interface {
	Path() string
	SetPath(path string)
	ConfigDir() string
	RootDir() string
	Load() (config.Bundle, error)
	LoadPlan() (config.Bundle, string, map[string]string, error)
	Hash() (string, error)
	Apply(ctx context.Context, bundle config.Bundle, sourceHashes map[string]string, mutate func(*config.Bundle) error) (config.Bundle, error)
}

type Deps struct {
	Ctx    context.Context
	Editor BundleEditor
	// ProjectID and RuntimeGeneration scope the Studio session to the runtime
	// that supplied its editor, snapshot, and catalog.
	ProjectID         int64
	RuntimeGeneration uint64
	// OpenDraft opens the staged bundle draft for this Studio session over the
	// editor port. The host supplies the engine; nil — like a nil Editor —
	// leaves the draft closed, which every caller already handles by falling
	// back to the active snapshot.
	OpenDraft      func(editor BundleEditor) (StudioDraft, error)
	Snapshot       *config.Snapshot
	Catalog        *config.Catalog
	Workflow       domain.Workflow
	Tasks          []domain.Task
	ConfigPath     string
	Reload         func(path string, diff []string) error
	CommandNames   []string
	ResolveCommand func(bundle config.Bundle, name string) (string, error)
	HookHistory    map[int][]studioprojection.HookExecuted // nil = empty history / unavailable
}

type studioRepos struct {
	Editor            BundleEditor
	ProjectID         int64
	RuntimeGeneration uint64
	OpenDraft         func(editor BundleEditor) (StudioDraft, error)
	Snapshot          *config.Snapshot
	Catalog           *config.Catalog
	ConfigPath        string
	CommandNames      []string
	ResolveCommand    func(bundle config.Bundle, name string) (string, error)
}

func (r studioRepos) activeSnapshot() *config.Snapshot { return r.Snapshot }

type Screen struct {
	id       screenhost.ID
	ctx      context.Context
	repos    studioRepos
	workflow domain.Workflow
	tasks    []domain.Task
	reload   func(string, []string) error
	styles   screenkit.Styles
	kit      screenkit.Kit

	grid                      screengrid.State
	studioDraft               StudioDraft
	studioFlowRow             int
	studioFlowCol             int
	studioFlowMessage         string
	studioGuardSet            int
	studioGuardIndex          int
	studioGuardType           int
	studioGuardMsg            string
	studioCommandIndex        int
	studioCommandField        int
	studioCommandMsg          string
	studioWorkflowIndex       int
	studioWorkflowField       int
	studioWorkflowMsg         string
	studioPersonaIndex        int
	studioPersonaRelatedIndex int
	studioPersonaMsg          string
	studioHookIndex           int
	studioHookHistoryIndex    int
	studioHookMsg             string
	hookHistory               map[int][]studioprojection.HookExecuted
	studioPreviewMsg          string
	studioApplyOpen           bool
	studioApplyMsg            string
	studioApplyArmed          bool
	studioApplyConfirmation   string
	projection                studioprojection.Projection
	projectionReady           bool
	projectionSnapshot        *config.Snapshot
	projectionTaskHead        *domain.Task
	projectionTaskCount       int
	commandPreview            *commandPreviewCache
	// boxes memoises the detail zone's composition. Shared across Screen copies
	// for the same reason commandPreview is: the body that fills it renders under
	// a value receiver. See [inspectorBoxCache].
	boxes *inspectorBoxCache

	// studioBody is the body the update path already rendered for this frame,
	// so View can paint it instead of rendering it a second time. Transient —
	// dropped by Lifecycle — and never part of State.
	studioBody studioFrame
}

// studioFrame is one Studio body: the items the arranger will window, and the
// item the renderer marked as the anchor.
//
// It used to carry both coordinate spaces at once — a source body and its
// cursor, plus the wrapped lines and the source-to-wrapped offset table — and
// they had to be memoized as ONE value, because caching the wrapped lines
// without the offsets that produced them would let an anchor be translated
// against a different wrap than the one on screen. There is one space now, so
// there is nothing left to keep in step: `anchor` indexes `items`.
type studioFrame struct {
	ready  bool
	key    studioFrameKey
	items  []string
	anchor int
}

// studioFrameKey is the identity of every input a Studio body is rendered from.
// It is compared with == and never dereferenced.
//
// Pointer and length identity is a sound "nothing changed" witness here because
// the root Model replaces its snapshot, workflow and task slices WHOLESALE on
// every reload (`m.tasks = snap.Tasks`, applyProjectRuntime rebuilds the
// workflow) rather than mutating them in place. A false NEGATIVE costs one
// re-render, i.e. exactly today's behaviour; a false POSITIVE would paint a
// stale screen, so every input the bodies read gets its own field.
//
// Theme and language are deliberately absent: both ride on the frame and change
// only through Settings, so returning to Studio afterwards is a navigation that
// fires LifecycleEnter and drops the memo. A resize does the same through
// LifecycleResize, and the width is keyed anyway.
//
// The row budget is deliberately absent, and it used to be here. It was keyed
// when the screen sliced its own viewport; no Studio body reads its rows — only
// its width — and it is the arranger, not the memo, that decides how many of the
// items get painted.
type studioFrameKey struct {
	id    screenhost.ID
	width int

	editor   BundleEditor
	snapshot *config.Snapshot
	catalog  *config.Catalog
	path     string

	// state carries the draft POINTER plus every cursor and message field the
	// bodies read. The candidate behind that pointer is mutable and a mutation
	// can leave the whole key untouched (press `a` twice on Workflow and only the
	// candidate moves), which is why the update path always RE-RENDERS and only
	// the view path is allowed to consult the memo.
	state State

	workflowID     int64
	workflowKey    string
	workflowName   string
	defaults       *domain.WorkflowDefaults
	buckets        int
	bucketHead     *domain.Bucket
	transitions    int
	transitionHead *domain.WorkflowTransition
	tasks          int
	taskHead       *domain.Task
}

// studioFrameKey is the memo key at the width the arranger will hand the
// section. studioFrameKeyAt is the form the section itself uses, where the
// width has already arrived on the Canvas.
func (s Screen) studioFrameKey() studioFrameKey {
	return s.studioFrameKeyAt(s.studioSectionWidth())
}

func (s Screen) studioFrameKeyAt(width int) studioFrameKey {
	key := studioFrameKey{
		id:           s.id,
		width:        width,
		editor:       s.repos.Editor,
		snapshot:     s.repos.Snapshot,
		catalog:      s.repos.Catalog,
		path:         s.repos.ConfigPath,
		state:        s.State(),
		workflowID:   s.workflow.ID,
		workflowKey:  s.workflow.Key,
		workflowName: s.workflow.Name,
		defaults:     s.workflow.Defaults,
		buckets:      len(s.workflow.Buckets),
		transitions:  len(s.workflow.Transitions),
		tasks:        len(s.tasks)}
	if len(s.workflow.Buckets) > 0 {
		key.bucketHead = &s.workflow.Buckets[0]
	}
	if len(s.workflow.Transitions) > 0 {
		key.transitionHead = &s.workflow.Transitions[0]
	}
	if len(s.tasks) > 0 {
		key.taskHead = &s.tasks[0]
	}
	return key
}

type State struct {
	Draft               StudioDraft
	FlowRow             int
	FlowCol             int
	FlowMessage         string
	GuardSet            int
	GuardIndex          int
	GuardType           int
	GuardMessage        string
	CommandIndex        int
	CommandField        int
	CommandMessage      string
	WorkflowIndex       int
	WorkflowField       int
	WorkflowMessage     string
	PersonaIndex        int
	PersonaRelatedIndex int
	PersonaMessage      string
	HookIndex           int
	HookHistoryIndex    int
	HookMessage         string
	PreviewMessage      string
	ApplyOpen           bool
	ApplyMessage        string
	ApplyArmed          bool
	ApplyConfirmation   string
}

func New() Screen {
	return Screen{
		grid:           screengrid.NewState(),
		commandPreview: &commandPreviewCache{},
		boxes:          &inspectorBoxCache{},
	}
}

func (s Screen) WithState(state State) Screen {
	s.studioDraft = state.Draft
	s.studioFlowRow, s.studioFlowCol, s.studioFlowMessage = state.FlowRow, state.FlowCol, state.FlowMessage
	s.studioGuardSet, s.studioGuardIndex, s.studioGuardType, s.studioGuardMsg = state.GuardSet, state.GuardIndex, state.GuardType, state.GuardMessage
	s.studioCommandIndex, s.studioCommandField, s.studioCommandMsg = state.CommandIndex, state.CommandField, state.CommandMessage
	s.studioWorkflowIndex, s.studioWorkflowField, s.studioWorkflowMsg = state.WorkflowIndex, state.WorkflowField, state.WorkflowMessage
	s.studioPersonaIndex, s.studioPersonaRelatedIndex, s.studioPersonaMsg = state.PersonaIndex, state.PersonaRelatedIndex, state.PersonaMessage
	s.studioHookIndex, s.studioHookHistoryIndex, s.studioHookMsg = state.HookIndex, state.HookHistoryIndex, state.HookMessage
	s.studioPreviewMsg, s.studioApplyOpen, s.studioApplyMsg, s.studioApplyArmed, s.studioApplyConfirmation = state.PreviewMessage, state.ApplyOpen, state.ApplyMessage, state.ApplyArmed, state.ApplyConfirmation
	s.projectionReady = false
	if s.studioDraft != nil || s.repos.Snapshot != nil {
		s.refreshProjection()
	}
	return s
}

func (s Screen) State() State {
	return State{
		Draft: s.studioDraft, FlowRow: s.studioFlowRow, FlowCol: s.studioFlowCol, FlowMessage: s.studioFlowMessage,
		GuardSet: s.studioGuardSet, GuardIndex: s.studioGuardIndex, GuardType: s.studioGuardType, GuardMessage: s.studioGuardMsg,
		CommandIndex: s.studioCommandIndex, CommandField: s.studioCommandField, CommandMessage: s.studioCommandMsg,
		WorkflowIndex: s.studioWorkflowIndex, WorkflowField: s.studioWorkflowField, WorkflowMessage: s.studioWorkflowMsg,
		PersonaIndex: s.studioPersonaIndex, PersonaRelatedIndex: s.studioPersonaRelatedIndex, PersonaMessage: s.studioPersonaMsg,
		HookIndex: s.studioHookIndex, HookHistoryIndex: s.studioHookHistoryIndex, HookMessage: s.studioHookMsg,
		PreviewMessage: s.studioPreviewMsg, ApplyOpen: s.studioApplyOpen, ApplyMessage: s.studioApplyMsg,
		ApplyArmed: s.studioApplyArmed, ApplyConfirmation: s.studioApplyConfirmation}
}

func (s Screen) Bind(id screenhost.ID, deps Deps) Screen {
	if s.studioScopeChanged(deps) {
		s = New()
	}
	s.id = id
	s.ctx = deps.Ctx
	s.repos = studioRepos{Editor: deps.Editor, ProjectID: deps.ProjectID, RuntimeGeneration: deps.RuntimeGeneration, OpenDraft: deps.OpenDraft, Snapshot: deps.Snapshot, Catalog: deps.Catalog, ConfigPath: deps.ConfigPath, CommandNames: append([]string(nil), deps.CommandNames...), ResolveCommand: deps.ResolveCommand}
	s.workflow = deps.Workflow
	s.tasks = deps.Tasks
	s.reload = deps.Reload
	s.hookHistory = deps.HookHistory
	s.openStudioDraft()
	if !s.projectionReady || s.projectionSnapshot != deps.Snapshot || s.projectionTaskCount != len(s.tasks) || taskHead(s.tasks) != s.projectionTaskHead {
		s.refreshProjection()
	}
	return s
}

func (s Screen) studioScopeChanged(deps Deps) bool {
	if s.id == "" {
		return false
	}
	return s.repos.ProjectID != deps.ProjectID ||
		s.repos.RuntimeGeneration != deps.RuntimeGeneration ||
		s.repos.Snapshot != deps.Snapshot ||
		s.repos.ConfigPath != deps.ConfigPath
}

func taskHead(tasks []domain.Task) *domain.Task {
	if len(tasks) == 0 {
		return nil
	}
	return &tasks[0]
}

// openStudioDraft builds the draft ONCE, when the screen is bound, and holds it
// in screen state for the rest of the Studio session.
//
// It used to be built inside studioPreviewReport, i.e. inside a render, three
// times per keystroke — two editor.Load() disk reads plus a full YAML bundle
// parse and an editor.Hash() each time. That was not only slow: two renders
// inside ONE keystroke could observe DIFFERENT on-disk state while the screen
// believed it held no draft at all, so the summary table, the diff and the
// prompt preview were free to disagree with each other on the same frame.
//
// Building it at entry makes the answer definite: a Studio session reads the
// bundle exactly once, and every surface in it describes that one bundle. If
// the config file changes on disk while Studio is open the screen keeps showing
// the bundle it opened — the same contract an unsaved candidate already had —
// and ctrl+s refuses the apply through StudioDraft.AssuranceSnapshot, whose
// baseline check reports "studio draft baseline changed on disk; reload Studio
// before applying".
//
// Errors are swallowed on purpose: a missing editor or an invalid config leaves
// the draft nil, which every caller already handles by falling back to the
// active snapshot. The next Bind retries, so fixing the file on disk and
// re-entering Studio recovers without a restart.
func (s *Screen) openStudioDraft() {
	if s.studioDraft != nil || s.repos.Editor == nil || s.repos.OpenDraft == nil {
		return
	}
	if draft, err := s.repos.OpenDraft(s.repos.Editor); err == nil {
		s.studioDraft = draft
	}
}

func (s Screen) withFrame(frame screenhost.Frame) Screen {
	s.kit = frame.Kit()
	s.styles = s.kit.Styles
	if s.studioDraft != nil {
		s.studioDraft.BindText(s.kit.T)
	}
	return s
}

func (s Screen) ID() screenhost.ID { return s.id }

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	s = s.withFrame(frame)
	if s.commandPreview == nil {
		s.commandPreview = &commandPreviewCache{}
	}
	if s.boxes == nil {
		s.boxes = &inspectorBoxCache{}
	}
	if ready, ok := msg.(commandPreviewMsg); ok {
		s.applyCommandPreview(ready)
		return screenhost.Stay(s, nil)
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	if s.studioApplyOpen {
		return screenhost.Stay(s, s.handleStudioApplyOverlayKey(key))
	}
	var cmd tea.Cmd
	switch s.id {
	case screenhost.StudioWorkflow:
		cmd = s.handleStudioWorkflowKey(key)
	case screenhost.StudioCommands:
		cmd = s.handleStudioCommandsKey(key)
	case screenhost.StudioPersonas:
		return s.handleStudioPersonasKey(key)
	case screenhost.StudioHooks:
		cmd = s.handleStudioHooksKey(key)
	}
	return screenhost.Stay(s, cmd)
}

// View arranges the one section and returns the body indented two columns
// inside the terminal, which is where a Studio panel has always sat.
//
// The six one-line renderStudioX wrappers are gone with the viewport they all
// called: what differed between them was never the layout, only the content, and
// content is now the section's. There is nothing left here to prepend, slice or
// charge for — Arrange spends the leading blank row itself.
func (s Screen) View(frame screenhost.Frame) string {
	s = s.withFrame(frame)
	var view string
	switch s.id {
	case screenhost.StudioWorkflow:
		view = s.paintStudioApplyOverlay(s.viewStudioWorkflow())
	case screenhost.StudioCommands:
		view = s.paintStudioApplyOverlay(s.viewStudioCommands())
	case screenhost.StudioPersonas:
		view = s.paintStudioApplyOverlay(s.viewStudioPersonas())
	case screenhost.StudioHooks:
		view = s.paintStudioApplyOverlay(s.viewStudioHooks())
	}
	return view
}

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if s.studioApplyOpen {
		return []screenhost.FooterBinding{
			{Key: "ctrl+s", Label: frame.Text("tui.studio.apply.footer.apply"), Primary: true},
			{Key: "esc", Label: frame.Text("tui.studio.apply.footer.discard")},
			frame.FooterHelp(false),
		}
	}
	return []screenhost.FooterBinding{
		frame.FooterRefresh(true),
		frame.FooterHistoryBack(false),
	}
}

func (s Screen) Help(screenhost.Frame) []screenhost.HelpGroup { return nil }

// Lifecycle is where the Studio session begins and where the frame memo is
// dropped. Bind has already opened the draft on the value that reaches here;
// what Lifecycle adds is that the host STORES this outcome, so the draft
// survives into the next frame instead of being rebuilt by the next Bind.
//
// The memo is dropped on every event because all five of them mean the frame it
// was rendered under is gone: enter/focus arrive with fresh host data, resize
// changes the wrap, and leave/blur must not leave a body behind for whatever
// re-enters later. The draft itself is never dropped — an unsaved candidate has
// to survive a sub-tab switch.
//
// A resize additionally RESYNCS the arranger. The body wraps to the new width,
// so the anchor and the offset the old width produced describe items that no
// longer exist at those indices; Arrange would clamp them for the frame it
// paints, but only Resync persists the clamp — and since #2448 a keystroke moves
// within the measurement the last resolve took, so a missed Resync costs a stale
// clamp rather than a single stale frame.
func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	s = s.withFrame(frame)
	s.studioBody = studioFrame{}
	s.openStudioDraft()
	s.refreshProjection()
	if s.commandPreview == nil {
		s.commandPreview = &commandPreviewCache{}
	}
	if s.boxes == nil {
		s.boxes = &inspectorBoxCache{}
	}
	_ = event
	var cmd tea.Cmd
	switch s.id {
	case screenhost.StudioWorkflow:
		s.resyncWorkflowGrid()
	case screenhost.StudioCommands:
		s.resyncCommandsGrid()
		cmd = s.commandPreviewCmd()
	case screenhost.StudioPersonas:
		s.resyncPersonasGrid()
	case screenhost.StudioHooks:
		s.resyncHooksGrid()
	}
	return screenhost.Stay(s, cmd)
}

// ResetOnNavigation opts the Studio routes into LifecycleEnter. Studio is a
// base route, not a modal one, so without this the host never tells it that a
// session started and the draft opened by Bind would be discarded with every
// unstored bound copy. It resets no selection: Lifecycle only clears the frame
// memo.
func (s Screen) ResetOnNavigation() bool { return true }

func studioKeyIn(key string, keys ...string) bool {
	for _, candidate := range keys {
		if key == candidate {
			return true
		}
	}
	return false
}

func (s Screen) t(key string) string { return s.kit.T(key) }

// Scroll is the focused list's scroll offset, held by the grid. It is an ITEM
// index — and a Studio list item is one row — so it means what it always meant.
//
// The four cases below are every Studio id there is (screenhost/contract.go:
// StudioWorkflow, StudioCommands, StudioPersonas, StudioHooks), and each names
// the list zone of that sub-screen's own screengrid root. The fallback used to
// read a body-scroll offset off a private screenlayout.State; nothing bound an
// id that reached it, and the state it read is gone.
func (s Screen) Scroll() int {
	if s.id == screenhost.StudioCommands {
		return s.grid.Layout().Offset(sectionCommandsList)
	}
	if s.id == screenhost.StudioWorkflow {
		return s.grid.Layout().Offset(sectionWorkflowList)
	}
	if s.id == screenhost.StudioPersonas {
		return s.grid.Layout().Offset(sectionPersonasList)
	}
	if s.id == screenhost.StudioHooks {
		return s.grid.Layout().Offset(sectionHooksList)
	}
	return 0
}

// StudioDraftOpen reports whether the screen is holding the draft it opened at
// entry. Exported for the host-level wiring test that pins AC1: the draft has
// to exist before the FIRST View of a Studio route, or that View falls back to
// the snapshot bundle and renders a different body.
func (s Screen) StudioDraftOpen() bool { return s.studioDraft != nil }

func (s Screen) InvalidateConfirmation() Screen {
	s.studioApplyArmed = false
	s.studioApplyConfirmation = ""
	s.studioApplyOpen = false
	s.studioApplyMsg = ""
	return s
}

var _ screenhost.Screen = Screen{}

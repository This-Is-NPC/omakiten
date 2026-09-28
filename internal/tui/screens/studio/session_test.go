package studio

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// countingBundleStore counts every LoadBundle and HashFile the Studio screen
// causes. Both are disk reads plus, for LoadBundle, a full YAML bundle parse —
// the cost the user feels as "desempenho muito ruim" when it lands on the
// render path.
type countingBundleStore struct {
	inner *configstore.Adapter
	loads atomic.Int64
	hashs atomic.Int64
}

func (s *countingBundleStore) LoadBundle(path string) (config.Bundle, error) {
	s.loads.Add(1)
	return s.inner.LoadBundle(path)
}

func (s *countingBundleStore) LoadBundlePlan(path string) (config.Bundle, map[string]string, error) {
	s.loads.Add(1)
	return s.inner.LoadBundlePlan(path)
}

func (s *countingBundleStore) SaveBundle(path string, bundle config.Bundle) error {
	return s.inner.SaveBundle(path, bundle)
}

func (s *countingBundleStore) HashFile(path string) (string, error) {
	s.hashs.Add(1)
	return s.inner.HashFile(path)
}

func (s *countingBundleStore) WriteAtomic(path string, data []byte) error {
	return s.inner.WriteAtomic(path, data)
}

func (s *countingBundleStore) RemoveFile(path string) error {
	return s.inner.RemoveFile(path)
}

func (s *countingBundleStore) ValidatePath(root, path string) error {
	return s.inner.ValidatePath(root, path)
}

func (s *countingBundleStore) EnsureDefaultFiles(rootDir string) error {
	return s.inner.EnsureDefaultFiles(rootDir)
}

func (s *countingBundleStore) ConfigRootFromYAMLPath(path string) string {
	return s.inner.ConfigRootFromYAMLPath(path)
}

// studioCountingSession drives the Studio screen the way the host does — bind,
// Update, store the outcome, bind again, View — over a bundle store that counts
// its own reads.
func studioCountingSession(tb testing.TB, id screenhost.ID) (*countingBundleStore, Deps) {
	tb.Helper()
	configPath := studioBenchConfigPath(tb)
	store := &countingBundleStore{inner: configstore.New()}
	return store, Deps{
		Ctx:        context.Background(),
		Editor:     bundleeditor.New(store, configPath),
		OpenDraft:  openFixtureBundleDraft,
		Workflow:   studioBenchWorkflow(),
		Tasks:      studioBenchTasks(40),
		ConfigPath: configPath,
	}
}

// TestStudioSessionReadsTheBundleOnce is the regression test for the render
// path building a StudioDraft.
//
// NewStudioDraft runs editor.Load() (disk read + full YAML parse) and
// editor.Hash() (a second read). Calling it from studioPreviewReport meant
// every render paid for both, three times per keystroke — and, worse, two
// renders inside ONE keystroke could observe DIFFERENT on-disk state while the
// screen believed it held no draft. A Studio session must read the bundle
// exactly once, at entry, and describe that one bundle for its whole life.
func TestStudioSessionReadsTheBundleOnce(t *testing.T) {
	t.Parallel()

	ids := map[string]screenhost.ID{
		"commands": screenhost.StudioCommands,
		"workflow": screenhost.StudioWorkflow,
		"personas": screenhost.StudioPersonas,
		"hooks":    screenhost.StudioHooks,
	}
	for name, id := range ids {
		name, id := name, id
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertStudioSessionReadsOnce(t, id)
		})
	}
}

func assertStudioSessionReadsOnce(t *testing.T, id screenhost.ID) {
	t.Helper()
	store, deps := studioCountingSession(t, id)
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(id, deps)
	entered, ok := screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if !ok {
		t.Fatal("Lifecycle did not carry a studio.Screen")
	}
	screen = entered
	if !screen.StudioDraftOpen() {
		t.Fatal("entering Studio left no draft open; every later render falls back to the snapshot bundle")
	}
	afterEntry := store.loads.Load()
	if afterEntry != 1 {
		t.Fatalf("screen entry read the bundle %d time(s), want exactly 1", afterEntry)
	}
	for _, key := range []string{"j", "k", "pgdown", "pgup", "down", "up", "G", "home"} {
		outcome := screen.Bind(id, deps).Update(frame, screentest.Key(key))
		next, ok := outcome.Screen.(Screen)
		if !ok {
			t.Fatalf("Update(%q) carried %T, not a studio.Screen", key, outcome.Screen)
		}
		screen = next
		_ = screen.Bind(id, deps).View(frame)
	}
	if got := store.loads.Load(); got != afterEntry {
		t.Fatalf("10 keystrokes read the bundle %d more time(s); the render path is still building a draft", got-afterEntry)
	}
	if got := store.hashs.Load(); got > afterEntry {
		t.Fatalf("10 keystrokes hashed the config %d time(s), want at most the %d from entry", got, afterEntry)
	}
}

// TestStudioViewPaintsWhatAFreshRenderWouldPaint pins acceptance criterion 5
// against the frame memo.
//
// The memo exists so View repaints the body Update already rendered instead of
// composing it a second time. The property that makes that safe is the only one
// worth asserting: for every Studio screen, at every point of a long key drive,
// the memoized View must be byte-identical to the View of the SAME screen with
// the memo cleared — i.e. to exactly what the pre-memo code rendered.
func TestStudioViewPaintsWhatAFreshRenderWouldPaint(t *testing.T) {
	t.Parallel()

	drives := map[screenhost.ID][]string{
		screenhost.StudioCommands: {"j", "down", "p", "w", "t", "x", "d", "pgdown", "k", "up", "G", "home"},
		screenhost.StudioWorkflow: {"down", "j", "a", "d", "pgdown", "G", "k", "up", "home"},
		screenhost.StudioPersonas: {"down", "j", "tab", "pgdown", "G", "k", "up", "home"},
		screenhost.StudioHooks:    {"down", "j", "tab", "pgdown", "G", "k", "up", "home"},
	}
	names := map[screenhost.ID]string{
		screenhost.StudioCommands: "commands",
		screenhost.StudioWorkflow: "workflow",
		screenhost.StudioPersonas: "personas",
		screenhost.StudioHooks:    "hooks",
	}
	for id, keys := range drives {
		id, keys := id, keys
		t.Run(names[id], func(t *testing.T) {
			t.Parallel()

			deps := studioBenchDeps(t)
			frame := screentest.FrameAt(t, 120, 40)
			screen := New().Bind(id, deps)
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)

			assertMemoAgreesWithFreshRender(t, screen.Bind(id, deps), frame, "entry")
			for i, key := range keys {
				outcome := screen.Bind(id, deps).Update(frame, screentest.Key(key))
				next, ok := outcome.Screen.(Screen)
				if !ok {
					t.Fatalf("Update(%q) carried %T, not a studio.Screen", key, outcome.Screen)
				}
				screen = next
				assertMemoAgreesWithFreshRender(t, screen.Bind(id, deps), frame, fmt.Sprintf("after %d key(s), last %q", i+1, key))
			}
		})
	}
}

// assertMemoAgreesWithFreshRender compares the screen's View against the View of
// the same screen with its frame memo cleared, and — where the memo is the one
// this frame would actually use — the ITEMS and the anchor as well.
//
// It used to compare both coordinate spaces, because bytes alone would pass
// while an anchor pointed at the wrong wrapped line. There is one space now, so
// the deep half is items plus the anchor that indexes them, and the anchor is
// checked against the item it selects rather than against a number.
func assertMemoAgreesWithFreshRender(tb testing.TB, screen Screen, frame screenhost.Frame, when string) {
	tb.Helper()

	fresh := screen
	fresh.studioBody = studioFrame{}
	// The deep comparison only applies when the memo is the one this frame
	// would actually paint. A memo whose key no longer matches is simply not
	// used — studioFrameForView re-renders — and holding it to the current
	// body would assert a property the design never claims.
	if screen.studioBody.ready && screen.studioBody.key == screen.studioFrameKey() {
		memo, plain := screen.studioBody, fresh.renderStudioFrame()
		if strings.Join(memo.items, "\n") != strings.Join(plain.items, "\n") {
			tb.Fatalf("%s: memoized items differ from a fresh render", when)
		}
		if memo.anchor != plain.anchor {
			tb.Fatalf("%s: memoized anchor %d differs from a fresh render's %d", when, memo.anchor, plain.anchor)
		}
		if memo.anchor >= 0 && memo.anchor < len(memo.items) && memo.items[memo.anchor] != plain.items[plain.anchor] {
			tb.Fatalf("%s: the memo's anchor selects %q, a fresh render's selects %q", when, memo.items[memo.anchor], plain.items[plain.anchor])
		}
	}
	if got, want := screen.View(frame), fresh.View(frame); got != want {
		tb.Fatalf("%s: the memoized view is not what a fresh render paints\n--- memo ---\n%s\n--- fresh ---\n%s",
			when, screentest.StripANSI(got), screentest.StripANSI(want))
	}
}

// TestStudioFrameMemoFollowsHostData is the other half of the memo's contract.
// Its key is compared, never dereferenced, so it has to name every host input a
// Studio body reads. If it misses one, the screen paints a body assembled from
// data the host has already replaced — a stale screen, which is strictly worse
// than the re-render it saves.
func TestStudioFrameMemoFollowsHostData(t *testing.T) {
	t.Parallel()

	frame := screentest.FrameAt(t, 120, 40)
	base := studioBenchDeps(t)

	// Workflow paints bucket names from the draft, not host Workflow. Host
	// task counts still reach the list: that is the remaining host input the
	// memo must follow.
	screen := New().Bind(screenhost.StudioWorkflow, base)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen = studioDrive(t, screen, frame, "j")

	before := screentest.StripANSI(screen.Bind(screenhost.StudioWorkflow, base).View(frame))

	moved := base
	moved.Tasks = studioBenchTasks(4)
	after := screentest.StripANSI(screen.Bind(screenhost.StudioWorkflow, moved).View(frame))
	if before == after {
		t.Fatal("replacing the task list did not change the Workflow body; the memo is serving host data the model has already dropped")
	}
}

// TestUntouchedStudioSaysTheCandidateIsClean pins the one deliberate behaviour
// change that follows from opening the draft at entry.
//
// The save/apply keys used to branch on "is there a draft at all", and with no
// draft they answered "no Studio <screen> changes to save" / "no Studio changes
// to apply". A draft now always exists whenever the editor is wired, so the
// untouched case falls through to the dirtiness check and answers "candidate is
// clean" instead. Same situation, more accurate sentence — and the old sentence
// still fires where it is now the true one: no editor, or a config the loader
// rejected, i.e. no draft could be opened.
func TestUntouchedStudioSaysTheCandidateIsClean(t *testing.T) {
	t.Parallel()

	frame := screentest.FrameAt(t, 120, 40)
	cases := []struct {
		name string
		id   screenhost.ID
		key  string
		read func(State) string
	}{
		{"commands", screenhost.StudioCommands, "ctrl+s", func(s State) string { return s.CommandMessage }},
		{"workflow", screenhost.StudioWorkflow, "ctrl+s", func(s State) string { return s.WorkflowMessage }},
		{"personas", screenhost.StudioPersonas, "ctrl+s", func(s State) string { return s.PersonaMessage }},
		{"hooks", screenhost.StudioHooks, "ctrl+s", func(s State) string { return s.HookMessage }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wired := studioBenchDeps(t)
			screen := New().Bind(tc.id, wired)
			screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			screen = studioDrive(t, screen, frame, tc.key)
			if got := tc.read(screen.State()); got != "candidate is clean" {
				t.Fatalf("an untouched Studio with a wired editor said %q, want %q", got, "candidate is clean")
			}

			// No editor: no draft can be opened, and the "nothing here to save"
			// sentence is the accurate one.
			bare := New().Bind(tc.id, Deps{Ctx: context.Background(), Workflow: studioBenchWorkflow()})
			bare = bare.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			bare = studioDrive(t, bare, frame, tc.key)
			if got := tc.read(bare.State()); !strings.HasPrefix(got, "no Studio ") {
				t.Fatalf("a Studio with no editor said %q, want the \"no Studio …\" sentence", got)
			}
		})
	}
}

// TestStudioDraftHoldsTheBundleItOpened states, as a test, what the screen now
// does when the config file changes underneath an open Studio.
//
// Before, a render with no draft re-read the bundle, so two renders inside one
// keystroke could describe two different files. Now the session is pinned to the
// bundle it opened: the preview keeps describing that bundle, and ctrl+s refuses
// to apply on top of a file that moved, naming the reload the user has to do.
func TestStudioDraftHoldsTheBundleItOpened(t *testing.T) {
	t.Parallel()

	deps := studioBenchDeps(t)
	frame := screentest.FrameAt(t, 120, 40)

	screen := New().Bind(screenhost.StudioCommands, deps)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	opened := screentest.StripANSI(screen.Bind(screenhost.StudioCommands, deps).View(frame))

	// Rewrite the bundle on disk, exactly as an editor in another window would.
	changed := studioBenchBundle()
	changed.Workflows[0].Buckets[0].Name = "Icebox"
	if err := config.SaveFullBundle(deps.ConfigPath, changed); err != nil {
		t.Fatalf("SaveFullBundle: %v", err)
	}

	held := screentest.StripANSI(screen.Bind(screenhost.StudioCommands, deps).View(frame))
	if held != opened {
		t.Fatalf("an on-disk change moved an open Studio preview; the session must describe the bundle it opened\n--- opened ---\n%s\n--- now ---\n%s", opened, held)
	}

	// Make the candidate dirty so ctrl+s reaches the baseline check rather than
	// stopping at "candidate is clean".
	screen.studioDraft.Mutate(func(bundle *config.Bundle) error {
		bundle.Workflows[0].Buckets[0].Name = "Backlog (edited)"
		return nil
	})
	screen = studioDrive(t, screen.Bind(screenhost.StudioCommands, deps), frame, "ctrl+s")
	if msg := screen.State().ApplyMessage; !strings.Contains(msg, "baseline changed on disk") {
		t.Fatalf("applying onto a moved file reported %q, want the baseline refusal", msg)
	}
}

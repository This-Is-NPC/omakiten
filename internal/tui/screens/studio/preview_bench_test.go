package studio

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// studioBenchSink keeps the compiler from eliding the rendered frame.
var studioBenchSink string

// BenchmarkStudioCommandsRender measures ONE keystroke on Studio › Commands the
// way the host actually drives it: bind live deps, Update, store the outcome,
// bind again, View. That round trip is the unit the user feels — the report is
// "desempenho muito ruim" while scrolling this screen — so anything cheaper
// than the full Bind→Update→Bind→View cycle would measure a path nobody runs.
//
// The bundle is written to a real temp directory and read back through
// configstore.Adapter, so editor.Load() and editor.Hash() perform the disk read
// and YAML parse the defect is about. An in-memory BundleStore would fake away
// the dominant cost and make the benchmark meaningless.
//
// The fixture is scaled to the shape a real Omakiten project ships (see
// studioBenchBundle): the full commandcatalog.CommandNames() binding set plus personas,
// skills, laws and templates carrying prose bodies, because Report() clones and
// diffs every one of those bodies twice per call.
//
// Two sub-benchmarks, because the two Commands key paths cost differently:
//   - scroll  — j/k/pgup/pgdown, the keys the user reported. These go to the
//     arranger, which resolves the frame the keystroke moves within; the body is
//     rendered once and the View that follows reuses it.
//   - command — j/k, which walk the command list and recompose the prompt preview.
func BenchmarkStudioCommandsRender(b *testing.B) {
	if testing.Verbose() {
		b.Log(studioBenchEnvironment())
	}
	deps := studioBenchDeps(b)
	frame := screentest.FrameAt(b, 120, 40)

	cases := []struct {
		name string
		keys []string
	}{
		{name: "scroll", keys: []string{"j", "j", "pgdown", "k", "pgup"}},
		{name: "command", keys: []string{"j", "j", "k"}},
	}
	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			screen := New().Bind(screenhost.StudioCommands, deps)
			entered, ok := screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
			if !ok {
				b.Fatal("Lifecycle did not carry a studio.Screen")
			}
			screen = entered

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				outcome := screen.
					Bind(screenhost.StudioCommands, deps).
					Update(frame, screentest.Key(tc.keys[i%len(tc.keys)]))
				next, ok := outcome.Screen.(Screen)
				if !ok {
					b.Fatalf("Update carried %T, not a studio.Screen", outcome.Screen)
				}
				screen = next
				studioBenchSink = screen.Bind(screenhost.StudioCommands, deps).View(frame)
			}
		})
	}
}

// studioBenchEnvironment qualifies a recorded figure the way the ClaimNext
// ceiling in this repo does: a number without its Go version, CPU and
// GOMAXPROCS cannot be compared against a later run.
func studioBenchEnvironment() string {
	return fmt.Sprintf("go=%s goos=%s goarch=%s cpus=%d gomaxprocs=%d",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0))
}

// studioBenchConfigPath materialises the fixture bundle on disk and returns the
// wiring-file path. Shared with the session tests, which need the same
// realistic bundle behind a counting store.
func studioBenchConfigPath(tb testing.TB) string {
	tb.Helper()

	configPath := filepath.Join(tb.TempDir(), "config", "omakase.yaml")
	bundle := studioBenchBundle()
	if err := config.SaveFullBundle(configPath, bundle); err != nil {
		tb.Fatalf("SaveFullBundle: %v", err)
	}
	if err := writeStudioBenchEntities(configPath, bundle); err != nil {
		tb.Fatal(err)
	}
	return configPath
}

// studioBenchDeps wires the Studio screen against a file-backed bundle editor.
func studioBenchDeps(tb testing.TB) Deps {
	tb.Helper()

	configPath := studioBenchConfigPath(tb)
	editor := bundleeditor.New(configstore.New(), configPath)
	if _, err := editor.Load(); err != nil {
		tb.Fatalf("editor.Load: %v", err)
	}
	return Deps{
		Ctx:        context.Background(),
		Editor:     editor,
		OpenDraft:  openFixtureBundleDraft,
		Workflow:   studioBenchWorkflow(),
		Tasks:      studioBenchTasks(120),
		ConfigPath: configPath,
	}
}

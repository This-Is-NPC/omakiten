package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func appendTopLevelYAML(t *testing.T, path, body string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\n"+body)...), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func loadSubtaskKitFixture(t *testing.T, rel string) Bundle {
	t.Helper()
	root := t.TempDir()
	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}
	configPath := filepath.Join(root, "config", "omakase.yaml")
	if rel != "" {
		appendTopLevelYAML(t, configPath, "subtask_kit: "+rel+"\n")
	}
	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle(%q) error = %v", rel, err)
	}
	return bundle
}

func TestLoadBundleLoadsSubtaskKit(t *testing.T) {
	bundle := loadSubtaskKitFixture(t, "izakaya.yaml")

	if bundle.SubtaskKit != "izakaya.yaml" {
		t.Fatalf("Bundle.SubtaskKit = %q, want izakaya.yaml", bundle.SubtaskKit)
	}
	if bundle.SubtaskBundle == nil {
		t.Fatal("Bundle.SubtaskBundle = nil, want loaded sub-kit bundle")
	}
	if got := bundle.SubtaskBundle.Kit.Key; got != "izakaya" {
		t.Fatalf("SubtaskBundle.Kit.Key = %q, want izakaya", got)
	}
	if bundle.SubtaskBundle.SubtaskBundle != nil {
		t.Fatal("SubtaskBundle.SubtaskBundle != nil, nested cascade must not load")
	}

	var sawWarning bool
	for _, warning := range bundle.Warnings {
		if strings.Contains(warning.Message, "mcp_commands: ignored at depth >=1; MCP always resolves at project root") && filepath.Base(warning.Path) == "izakaya.yaml" {
			sawWarning = true
			break
		}
	}
	if !sawWarning {
		t.Fatalf("LoadBundle() warnings = %+v, want sub-kit mcp_commands warning", bundle.Warnings)
	}

	snap := BuildSnapshot(bundle)
	if got := snap.Kit().Key; got != "omakase" {
		t.Fatalf("Snapshot.Kit().Key = %q, want omakase", got)
	}
	sub, ok := snap.SubtaskKit()
	if !ok {
		t.Fatal("Snapshot.SubtaskKit() ok = false, want true")
	}
	if got := snap.SubtaskKitPath(); got != "izakaya.yaml" {
		t.Fatalf("Snapshot.SubtaskKitPath() = %q, want izakaya.yaml", got)
	}
	if got := sub.Kit().Key; got != "izakaya" {
		t.Fatalf("sub Snapshot.Kit().Key = %q, want izakaya", got)
	}
	if commands := sub.MCPCommands(); len(commands) != 0 {
		t.Fatalf("sub Snapshot.MCPCommands() = %+v, want empty because sub-kit mcp_commands are ignored", commands)
	}
}

func TestLoadBundleRejectsInvalidSubtaskKitPaths(t *testing.T) {
	cases := map[string]struct {
		rel     string
		wantErr string
	}{
		"absolute path": {rel: filepath.Join(string(os.PathSeparator), "tmp", "sub.yaml"), wantErr: "must be relative"},
		"parent escape": {rel: "../izakaya.yaml", wantErr: "must not contain parent directory segments"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := EnsureDefaultFiles(root); err != nil {
				t.Fatalf("EnsureDefaultFiles() error = %v", err)
			}
			configPath := filepath.Join(root, "config", "omakase.yaml")
			appendTopLevelYAML(t, configPath, "subtask_kit: "+tc.rel+"\n")

			_, err := LoadBundle(configPath)
			if err == nil {
				t.Fatalf("LoadBundle() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), "subtask_kit") || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadBundle() error = %q, want subtask_kit + %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoadBundleRejectsSubtaskKitSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}
	configPath := filepath.Join(root, "config", "omakase.yaml")

	// Target lives outside the config dir but inside the test tempdir so the
	// EvalSymlinks call resolves successfully. The lexical guards in
	// resolveSubtaskKitPath pass because the symlink itself is named
	// `escape.yaml` (no `..`, not absolute) — only the canonical-path check
	// detects the escape.
	outside := filepath.Join(root, "outside.yaml")
	if err := os.WriteFile(outside, []byte("version: 1\nkit:\n  id: 9\n  key: out\n  name: Out\nconfig:\n  workflow:\n    active: out\nworkflows:\n  - id: 9\n    key: out\n    name: Out\n    buckets:\n      - { id: 1, key: backlog, name: Backlog, position: 1 }\n    transitions: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside): %v", err)
	}
	symlinkPath := filepath.Join(root, "config", "escape.yaml")
	if err := os.Symlink(outside, symlinkPath); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	appendTopLevelYAML(t, configPath, "subtask_kit: escape.yaml\n")

	_, err := LoadBundle(configPath)
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want symlink-escape rejection")
	}
	if !strings.Contains(err.Error(), "subtask_kit") || !strings.Contains(err.Error(), "escapes config directory") {
		t.Fatalf("LoadBundle() error = %q, want subtask_kit + escapes config directory", err.Error())
	}
}

func TestLoadBundleRejectsMissingPartialAndNestedSubtaskKit(t *testing.T) {
	cases := map[string]struct {
		setup   func(t *testing.T, root string) string
		wantErr string
	}{
		"missing file": {
			setup: func(t *testing.T, root string) string {
				return "missing.yaml"
			},
			wantErr: "missing.yaml",
		},
		"partial file": {
			setup: func(t *testing.T, root string) string {
				writeFile(t, filepath.Join(root, "config", "partial.yaml"), "version: 1\nkit: {id: 200, key: partial, name: Partial}\n")
				return "partial.yaml"
			},
			wantErr: "config block is required",
		},
		"nested subtask kit": {
			setup: func(t *testing.T, root string) string {
				src := filepath.Join(root, "config", "izakaya.yaml")
				raw, err := os.ReadFile(src)
				if err != nil {
					t.Fatalf("ReadFile(%s): %v", src, err)
				}
				dst := filepath.Join(root, "config", "nested.yaml")
				if err := os.WriteFile(dst, append(raw, []byte("\nsubtask_kit: omakase.yaml\n")...), 0o644); err != nil {
					t.Fatalf("WriteFile(%s): %v", dst, err)
				}
				return "nested.yaml"
			},
			wantErr: "nested subtask_kit is not supported",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := EnsureDefaultFiles(root); err != nil {
				t.Fatalf("EnsureDefaultFiles() error = %v", err)
			}
			configPath := filepath.Join(root, "config", "omakase.yaml")
			appendTopLevelYAML(t, configPath, "subtask_kit: "+tc.setup(t, root)+"\n")

			_, err := LoadBundle(configPath)
			if err == nil {
				t.Fatalf("LoadBundle() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), "subtask_kit") || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadBundle() error = %q, want subtask_kit + %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestNewSubtaskKitNoticeNeeded(t *testing.T) {
	without := BuildSnapshot(loadSubtaskKitFixture(t, ""))
	withIzakaya := BuildSnapshot(loadSubtaskKitFixture(t, "izakaya.yaml"))
	withKaiseki := BuildSnapshot(loadSubtaskKitFixture(t, "kaiseki.yaml"))

	cases := map[string]struct {
		prev *Snapshot
		next *Snapshot
		want bool
	}{
		"nil previous is not an enablement transition": {prev: nil, next: withIzakaya, want: false},
		"enablement":         {prev: without, next: withIzakaya, want: true},
		"same subkit reload": {prev: withIzakaya, next: withIzakaya, want: false},
		"subkit swap":        {prev: withIzakaya, next: withKaiseki, want: false},
		"disable":            {prev: withIzakaya, next: without, want: false},
		"still absent":       {prev: without, next: without, want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := NewSubtaskKitNoticeNeeded(tc.prev, tc.next); got != tc.want {
				t.Fatalf("NewSubtaskKitNoticeNeeded() = %v, want %v", got, tc.want)
			}
		})
	}

	if got := SubtaskKitTransparencyNoticeKey(); got != "notice.subtask_kit.enabled.mcp_resolves_at_root" {
		t.Fatalf("SubtaskKitTransparencyNoticeKey() = %q", got)
	}
}

func TestSnapshotForResolvesRootAndSubtaskKits(t *testing.T) {
	withSub := BuildSnapshot(Bundle{
		Kit:    Kit{Key: "root"},
		Config: Settings{Workflow: WorkflowSettings{Active: "root"}},
		Workflows: []Workflow{{
			ID:   1,
			Key:  "root",
			Name: "Root",
			Buckets: []Bucket{
				{ID: 1, Key: "root-backlog", Name: "Root backlog", Position: 1},
			},
		}},
		SubtaskBundle: &Bundle{
			Kit:    Kit{Key: "sub"},
			Config: Settings{Workflow: WorkflowSettings{Active: "sub"}},
			Workflows: []Workflow{{
				ID:   2,
				Key:  "sub",
				Name: "Sub",
				Buckets: []Bucket{
					{ID: 10, Key: "sub-backlog", Name: "Sub backlog", Position: 1},
				},
			}},
		},
	})

	parentID := int64(42)
	cases := map[string]struct {
		task domain.Task
		want string
	}{
		"zero-value task defaults to root": {task: domain.Task{}, want: "root"},
		"root task":                        {task: domain.Task{ID: 1}, want: "root"},
		"depth one subtask":                {task: domain.Task{ID: 2, ParentID: &parentID}, want: "sub"},
		"depth two plus subtask":           {task: domain.Task{ID: 3, ParentID: &parentID}, want: "sub"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := withSub.For(tc.task)
			if got.Kit().Key != tc.want {
				t.Fatalf("Snapshot.For(...).Kit().Key = %q, want %q", got.Kit().Key, tc.want)
			}
		})
	}

	withoutSub := BuildSnapshot(Bundle{
		Kit:    Kit{Key: "root"},
		Config: Settings{Workflow: WorkflowSettings{Active: "root"}},
		Workflows: []Workflow{{
			ID:      1,
			Key:     "root",
			Name:    "Root",
			Buckets: []Bucket{{ID: 1, Key: "root-backlog", Name: "Root backlog", Position: 1}},
		}},
	})
	if got := withoutSub.For(domain.Task{ID: 4, ParentID: &parentID}).Kit().Key; got != "root" {
		t.Fatalf("Snapshot.For(subtask without sub-kit).Kit().Key = %q, want root", got)
	}
}

package config

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/defaults"
)

// withCanonicalSurfaces appends the shipped surfaces table to a
// wiring YAML fixture that omitted it, so LoadBundle completeness checks
// pass. Fixtures that already declare surfaces: are left unchanged.
func withCanonicalSurfaces(src string) string {
	trimmed := strings.TrimSpace(src)
	if strings.HasPrefix(trimmed, "surfaces:") || strings.Contains(src, "\nsurfaces:") {
		return src
	}
	return strings.TrimRight(src, "\n") + "\n" + canonicalSurfacesYAML()
}

func writeWiring(t *testing.T, path, contents string) {
	t.Helper()
	writeFile(t, path, withCanonicalSurfaces(contents))
}

func canonicalSurfacesYAML() string {
	type wrap struct {
		Surfaces SurfaceTable `yaml:"surfaces"`
	}
	data, err := yaml.Marshal(wrap{Surfaces: CanonicalSurfaceTable()})
	if err != nil {
		panic("canonicalSurfacesYAML: " + err.Error())
	}
	return string(data)
}

func TestDiffSurfacesCanonicalIsEmpty(t *testing.T) {
	missing, extra := DiffSurfaces(CanonicalSurfaceTable())
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("DiffSurfaces(canonical) missing=%v extra=%v, want empty", missing, extra)
	}
}

func TestDiffSurfacesReportsMissingAndExtra(t *testing.T) {
	got := CanonicalSurfaceTable()
	delete(got, "task.delete")
	tru := true
	got["task.delet"] = SurfacePolicy{CLI: &tru, TUI: &tru, HTTP: &tru}

	missing, extra := DiffSurfaces(got)
	if len(missing) != 1 || missing[0] != "task.delete" {
		t.Fatalf("missing = %v, want [task.delete]", missing)
	}
	if len(extra) != 1 || extra[0] != "task.delet" {
		t.Fatalf("extra = %v, want [task.delet]", extra)
	}
}

func TestSurfaceScaffoldYAMLMatchesCensus(t *testing.T) {
	raw := SurfaceScaffoldYAML()
	dec := yaml.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.KnownFields(true)
	var table SurfaceTable
	if err := dec.Decode(&table); err != nil {
		t.Fatalf("decode scaffold: %v\n%s", err, raw)
	}
	if err := validateSurfaces(table); err != nil {
		t.Fatalf("scaffold table: %v", err)
	}
	want := CanonicalSurfaceTable()
	if len(table) != len(want) {
		t.Fatalf("scaffold rows = %d, want %d", len(table), len(want))
	}
	for slug, wantRow := range want {
		got, ok := table[slug]
		if !ok {
			t.Errorf("scaffold missing %q", slug)
			continue
		}
		if surfaceBoolVal(got.CLI) != surfaceBoolVal(wantRow.CLI) ||
			surfaceBoolVal(got.TUI) != surfaceBoolVal(wantRow.TUI) ||
			surfaceBoolVal(got.HTTP) != surfaceBoolVal(wantRow.HTTP) {
			t.Errorf("%s: cli/tui/http = %v/%v/%v, want %v/%v/%v", slug,
				surfaceBoolVal(got.CLI), surfaceBoolVal(got.TUI), surfaceBoolVal(got.HTTP),
				surfaceBoolVal(wantRow.CLI), surfaceBoolVal(wantRow.TUI), surfaceBoolVal(wantRow.HTTP))
		}
		if got.Reason != wantRow.Reason {
			t.Errorf("%s: reason = %q, want %q", slug, got.Reason, wantRow.Reason)
		}
	}
	if !strings.Contains(raw, "command.list:") {
		t.Fatal("scaffold missing command.list")
	}
	if !strings.Contains(raw, "command.list: { cli: true, tui: true, http: true }\ncommand.resolve: { cli: true, tui: true, http: true }\n\ncomment.add:") {
		t.Fatal("scaffold should group by entity with a blank line between command and comment")
	}
	if !strings.Contains(raw, `wiring.set_snapshot: { cli: false, tui: false, http: false, reason: "${{intl:operations.denied.wiring}}" }`) {
		t.Fatal("scaffold wiring row missing reason token")
	}
}

func TestCanonicalSurfaceCensusIsClosed(t *testing.T) {
	if got := len(CanonicalSurfaceCensus); got != CanonicalSurfaceCount {
		t.Fatalf("CanonicalSurfaceCensus len = %d, want %d", got, CanonicalSurfaceCount)
	}
	seen := map[string]struct{}{}
	var product, destructive, wiring int
	for _, e := range CanonicalSurfaceCensus {
		if e.Slug == "" {
			t.Fatal("census entry with empty slug")
		}
		if _, dup := seen[e.Slug]; dup {
			t.Fatalf("duplicate census slug %q", e.Slug)
		}
		seen[e.Slug] = struct{}{}
		switch e.Kind {
		case SurfaceKindProduct:
			product++
		case SurfaceKindDestructive:
			destructive++
		case SurfaceKindWiring:
			wiring++
		default:
			t.Fatalf("slug %q: unknown kind %q", e.Slug, e.Kind)
		}
	}
	if product != 68 || destructive != 2 || wiring != 9 {
		t.Fatalf("census split product=%d destructive=%d wiring=%d, want 68/2/9", product, destructive, wiring)
	}
}

func TestValidateSurfacesAcceptsCanonicalTable(t *testing.T) {
	b := validTestBundle()
	if err := ValidateBundle(b, b.Skills, b.Laws, b.Personas, b.Templates); err != nil {
		t.Fatalf("ValidateBundle(canonical surfaces) = %v", err)
	}
}

func TestValidateSurfacesRejectsMissingCensusSlug(t *testing.T) {
	b := validTestBundle()
	delete(b.Surfaces, "task.delete")
	err := ValidateBundle(b, b.Skills, b.Laws, b.Personas, b.Templates)
	if err == nil {
		t.Fatal("ValidateBundle() error = nil, want missing census slug")
	}
	if !strings.Contains(err.Error(), "missing operation") || !strings.Contains(err.Error(), "task.delete") {
		t.Fatalf("ValidateBundle() error = %q, want missing operation task.delete", err)
	}
}

func TestValidateSurfacesRejectsUnknownSlug(t *testing.T) {
	b := validTestBundle()
	tru := true
	b.Surfaces["task.delet"] = SurfacePolicy{CLI: &tru, TUI: &tru, HTTP: &tru}
	err := ValidateBundle(b, b.Skills, b.Laws, b.Personas, b.Templates)
	if err == nil {
		t.Fatal("ValidateBundle() error = nil, want unknown slug")
	}
	if !strings.Contains(err.Error(), "task.delet") || !strings.Contains(err.Error(), "unknown operation slug") {
		t.Fatalf("ValidateBundle() error = %q, want unknown operation slug task.delet", err)
	}
}

func TestValidateSurfacesRejectsOmittedSurfaceKey(t *testing.T) {
	b := validTestBundle()
	row := b.Surfaces["task.transition"]
	row.CLI = nil
	b.Surfaces["task.transition"] = row
	err := ValidateBundle(b, b.Skills, b.Laws, b.Personas, b.Templates)
	if err == nil {
		t.Fatal("ValidateBundle() error = nil, want omitted cli")
	}
	if !strings.Contains(err.Error(), "task.transition") || !strings.Contains(err.Error(), "cli is required") {
		t.Fatalf("ValidateBundle() error = %q, want tui is required", err)
	}
}

func TestValidateSurfacesRejectsOmittedHTTPKey(t *testing.T) {
	b := validTestBundle()
	row := b.Surfaces["task.transition"]
	row.HTTP = nil
	b.Surfaces["task.transition"] = row
	err := ValidateBundle(b, b.Skills, b.Laws, b.Personas, b.Templates)
	if err == nil || !strings.Contains(err.Error(), "surfaces.task.transition: http is required") {
		t.Fatalf("ValidateBundle() error = %v, want http is required on task.transition", err)
	}
}

func TestValidateSurfacesRejectsFalseWithoutReason(t *testing.T) {
	b := validTestBundle()
	row := b.Surfaces["task.delete"]
	fls := false
	row.CLI = &fls
	row.Reason = ""
	b.Surfaces["task.delete"] = row
	err := ValidateBundle(b, b.Skills, b.Laws, b.Personas, b.Templates)
	if err == nil {
		t.Fatal("ValidateBundle() error = nil, want missing reason")
	}
	if !strings.Contains(err.Error(), "task.delete") || !strings.Contains(err.Error(), "reason is required") {
		t.Fatalf("ValidateBundle() error = %q, want reason is required", err)
	}
}

func TestShippedSurfacesModuleMatchesCensus(t *testing.T) {
	raw, err := defaults.FS.ReadFile("config/modules/surfaces.yaml")
	if err != nil {
		t.Fatalf("read shipped surfaces module: %v", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var table SurfaceTable
	if err := dec.Decode(&table); err != nil {
		t.Fatalf("decode shipped surfaces module: %v", err)
	}
	if err := validateSurfaces(table); err != nil {
		t.Fatalf("shipped surfaces module: %v", err)
	}
	want := CanonicalSurfaceTable()
	if len(table) != len(want) {
		t.Fatalf("shipped surfaces rows = %d, want %d", len(table), len(want))
	}
	for slug, wantRow := range want {
		got, ok := table[slug]
		if !ok {
			t.Errorf("shipped module missing %q", slug)
			continue
		}
		if surfaceBoolVal(got.CLI) != surfaceBoolVal(wantRow.CLI) ||
			surfaceBoolVal(got.TUI) != surfaceBoolVal(wantRow.TUI) ||
			surfaceBoolVal(got.HTTP) != surfaceBoolVal(wantRow.HTTP) {
			t.Errorf("%s: cli/tui/http = %v/%v/%v, want %v/%v/%v", slug,
				surfaceBoolVal(got.CLI), surfaceBoolVal(got.TUI), surfaceBoolVal(got.HTTP),
				surfaceBoolVal(wantRow.CLI), surfaceBoolVal(wantRow.TUI), surfaceBoolVal(wantRow.HTTP))
		}
		if got.Reason != wantRow.Reason {
			t.Errorf("%s: reason = %q, want %q", slug, got.Reason, wantRow.Reason)
		}
	}
}

func surfaceBoolVal(p *bool) string {
	if p == nil {
		return "<omitted>"
	}
	if *p {
		return "true"
	}
	return "false"
}

func TestShippedKitsLoadCompleteSurfaceTable(t *testing.T) {
	for _, preset := range []string{"omakase"} {
		t.Run(preset, func(t *testing.T) {
			assertShippedKitSurfaceTable(t, preset)
		})
	}
}

func assertShippedKitSurfaceTable(t *testing.T, preset string) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() = %v", err)
	}
	bundle, err := LoadBundle(filepath.Join(tmp, "config", preset+".yaml"))
	if err != nil {
		t.Fatalf("LoadBundle(%s) = %v", preset, err)
	}
	if len(bundle.Surfaces) != CanonicalSurfaceCount {
		t.Fatalf("Surfaces len = %d, want %d", len(bundle.Surfaces), CanonicalSurfaceCount)
	}
	if err := validateSurfaces(bundle.Surfaces); err != nil {
		t.Fatalf("validateSurfaces(%s) = %v", preset, err)
	}
	row, ok := bundle.Surfaces["wiring.set_snapshot"]
	if !ok {
		t.Fatal("missing wiring.set_snapshot")
	}
	assertDeniedSnapshotSurface(t, row)
	assertEnabledTransitionSurface(t, bundle.Surfaces["task.transition"])
}

func assertDeniedSnapshotSurface(t *testing.T, row SurfacePolicy) {
	if row.CLI == nil || *row.CLI || row.TUI == nil || *row.TUI {
		t.Fatalf("wiring.set_snapshot = %+v, want all false", row)
	}
	if row.Reason != DeniedWiringReason {
		t.Fatalf("wiring.set_snapshot reason = %q, want %q", row.Reason, DeniedWiringReason)
	}
}

func assertEnabledTransitionSurface(t *testing.T, row SurfacePolicy) {
	if row.CLI == nil || !*row.CLI || row.TUI == nil || !*row.TUI {
		t.Fatalf("task.transition = %+v, want all true", row)
	}
}

func TestLoadBundleRejectsOmittedSurfaceKeyInYAML(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() = %v", err)
	}
	table := CanonicalSurfaceTable()
	row := table["command.resolve"]
	row.CLI = nil
	table["command.resolve"] = row
	data, err := yaml.Marshal(table)
	if err != nil {
		t.Fatalf("marshal surfaces: %v", err)
	}
	writeFile(t, filepath.Join(tmp, "config", "modules", "surfaces.yaml"), string(data))

	_, err = LoadBundle(filepath.Join(tmp, "config", "omakase.yaml"))
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want omitted cli")
	}
	if !strings.Contains(err.Error(), "command.resolve") || !strings.Contains(err.Error(), "cli is required") {
		t.Fatalf("LoadBundle() error = %q, want tui is required on command.resolve", err)
	}
}

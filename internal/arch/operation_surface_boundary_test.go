package arch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/internal/config"
	"omakiten/internal/testutil"
)

// The operation surface is the census, not every exported
// operation.Service method. Service still carries TUI extras outside
// that table; reflecting the type would snapshot the wrong contract.
// CanonicalSurfaceCensus is the single source of truth — validator,
// scaffold, --check, and this golden all share it.
//
// The listing is census order (already grouped by entity, product then
// wiring), one `slug kind` line, compared byte-exact. A deliberate
// census change shows up as a reviewable diff; an accidental one fails.

const operationSurfaceGolden = "operation_surface.golden"

func TestOperationSurfaceMatchesItsGolden(t *testing.T) {
	listing := operationSurfaceListing()
	assertOperationSurfaceListingNotVacuous(t, listing)
	testutil.Golden(t, operationSurfaceGolden, listing)
}

// assertOperationSurfaceListingNotVacuous refuses to pass on an empty
// census. An empty listing compares equal to an empty golden — green,
// and covering nothing. The floor is CanonicalSurfaceCount itself,
// so shrinking the census without updating the constant still fails.
func assertOperationSurfaceListingNotVacuous(t *testing.T, listing string) {
	t.Helper()
	if got, want := config.CanonicalSurfaceCount, 75; got != want {
		t.Fatalf("CanonicalSurfaceCount = %d, want %d — the census floor moved without a reviewable golden", got, want)
	}
	if got, want := len(config.CanonicalSurfaceCensus), config.CanonicalSurfaceCount; got != want {
		t.Fatalf("CanonicalSurfaceCensus len = %d, want CanonicalSurfaceCount %d", got, want)
	}
	lines := listingLines(listing)
	if got, want := len(lines), config.CanonicalSurfaceCount; got != want {
		t.Fatalf("operation surface listing has %d lines, want %d — an empty golden cannot pass", got, want)
	}
}

func operationSurfaceListing() string {
	var b strings.Builder
	for _, e := range config.CanonicalSurfaceCensus {
		b.WriteString(e.Slug)
		b.WriteByte(' ')
		b.WriteString(string(e.Kind))
		b.WriteByte('\n')
	}
	return b.String()
}

func listingLines(listing string) []string {
	trimmed := strings.TrimSuffix(listing, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestVersionedPresetSurfacesMatchCensus is A4: every shipped kit YAML
// under defaults/config/ must DiffSurfaces-empty against the census,
// the same check `okt config surfaces --check` runs via LoadSurfaceTable.
// The shared module is asserted on its own so a kit that dropped the
// `from:` include cannot hide behind a still-correct module file, and
// a drifted module cannot hide behind kits that stopped including it.
func TestVersionedPresetSurfacesMatchCensus(t *testing.T) {
	root := repoRootOrFail(t)

	t.Run("modules/surfaces.yaml", func(t *testing.T) {
		table := loadShippedSurfacesModule(t, root)
		assertDiffSurfacesEmpty(t, "defaults/config/modules/surfaces.yaml", table)
	})

	kits := shippedKitYAML(t, root)
	if len(kits) != 1 {
		t.Fatalf("found %d kit YAML files under defaults/config/, want the single Omakase fixture", len(kits))
	}
	for _, kit := range kits {
		name := filepath.Base(kit)
		t.Run(name, func(t *testing.T) {
			table, err := config.LoadSurfaceTable(kit)
			if err != nil {
				t.Fatalf("LoadSurfaceTable(%s): %v", name, err)
			}
			assertDiffSurfacesEmpty(t, name, table)
		})
	}
}

// TestVersionedPresetSurfacesCheckRejectsTruncatedTable seeds the
// --check comparison with a table missing one census slug. If
// DiffSurfaces stopped reporting missing rows, versioned presets could
// shrink in silence and this gate would stay green.
func TestVersionedPresetSurfacesCheckRejectsTruncatedTable(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	delete(table, "task.delete")
	missing, extra := config.DiffSurfaces(table)
	if len(missing) == 0 {
		t.Fatal("truncated table reported no missing slugs; DiffSurfaces is vacuous")
	}
	if len(missing) != 1 || missing[0] != "task.delete" {
		t.Fatalf("missing = %v, want [task.delete]", missing)
	}
	if len(extra) != 0 {
		t.Fatalf("extra = %v, want empty", extra)
	}
}

func assertDiffSurfacesEmpty(t *testing.T, name string, table config.SurfaceTable) {
	t.Helper()
	missing, extra := config.DiffSurfaces(table)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("%s: okt config surfaces --check would fail: missing=%v extra=%v", name, missing, extra)
	}
}

func shippedKitYAML(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, "defaults", "config")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var kits []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		kits = append(kits, filepath.Join(dir, e.Name()))
	}
	return kits
}

func loadShippedSurfacesModule(t *testing.T, root string) config.SurfaceTable {
	t.Helper()
	path := filepath.Join(root, "defaults", "config", "modules", "surfaces.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var table config.SurfaceTable
	if err := dec.Decode(&table); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return table
}

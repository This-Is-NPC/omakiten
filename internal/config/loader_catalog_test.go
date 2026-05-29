package config

import (
	"path/filepath"
	"testing"
)

// TestBuildCatalog pins the fold contract: every loaded entry is emitted in
// load order, picked entries are flagged Active=true and carry the picked
// copy's metadata, and the rest are flagged Active=false.
func TestBuildCatalog(t *testing.T) {
	type item struct {
		slug   string
		scope  string
		active bool
	}
	loaded := []item{{slug: "a"}, {slug: "b"}, {slug: "c"}}
	// Picked carries metadata ("scope") the pick step stamped; the catalog
	// must surface that copy for active entries, not the raw loaded one.
	picked := []item{{slug: "a", scope: "global"}, {slug: "c", scope: "persona"}}

	got := buildCatalog(loaded, picked,
		func(i item) string { return i.slug },
		func(i item, a bool) item { i.active = a; return i })

	want := []item{
		{slug: "a", scope: "global", active: true},
		{slug: "b", scope: "", active: false},
		{slug: "c", scope: "persona", active: true},
	}
	if len(got) != len(want) {
		t.Fatalf("buildCatalog len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buildCatalog[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestLoadBundleCatalogFlagsActive pins the Settings-view contract: LoadBundle
// exposes the full on-disk entity catalog via All*, strictly larger than the
// active picked subset, with each picked entry flagged Active and at least one
// non-wired entry flagged inactive.
func TestLoadBundleCatalogFlagsActive(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}
	// omakase wires the Naruto roster — a subset of the shared persona pool.
	bundle, err := LoadBundle(filepath.Join(tmp, "config", "omakase.yaml"))
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}

	if len(bundle.AllPersonas) <= len(bundle.Personas) {
		t.Fatalf("AllPersonas (%d) must exceed picked Personas (%d): catalog should list every preset's personas",
			len(bundle.AllPersonas), len(bundle.Personas))
	}

	activeSlugs := map[string]struct{}{}
	for _, p := range bundle.Personas {
		activeSlugs[p.Slug] = struct{}{}
	}

	var sawInactive bool
	for _, p := range bundle.AllPersonas {
		_, wired := activeSlugs[p.Slug]
		if p.Active != wired {
			t.Fatalf("AllPersonas[%s].Active = %v, want %v (wired=%v)", p.Slug, p.Active, wired, wired)
		}
		if !wired {
			sawInactive = true
		}
	}
	if !sawInactive {
		t.Fatal("AllPersonas carried no inactive entry; catalog should include non-wired personas")
	}

	// Same contract for skills: the shared skill pool dwarfs any one preset.
	if len(bundle.AllSkills) < len(bundle.Skills) {
		t.Fatalf("AllSkills (%d) < picked Skills (%d)", len(bundle.AllSkills), len(bundle.Skills))
	}
	skillActive := map[string]struct{}{}
	for _, s := range bundle.Skills {
		skillActive[s.Slug] = struct{}{}
	}
	for _, s := range bundle.AllSkills {
		_, wired := skillActive[s.Slug]
		if s.Active != wired {
			t.Fatalf("AllSkills[%s].Active = %v, want %v", s.Slug, s.Active, wired)
		}
	}
}

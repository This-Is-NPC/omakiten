package app

import (
	"testing"

	"omakiten/internal/config"
)

// TestAllPersonasFromSnapshotCarriesActive pins the app-layer mapping: the
// Settings projection sources the full catalog (Bundle.AllPersonas) and
// carries each entry's Active flag into the domain shape, while the active-only
// projection stays scoped to the picked set.
func TestAllPersonasFromSnapshotCarriesActive(t *testing.T) {
	bundle := config.Bundle{
		Personas: []config.Persona{
			{Slug: "wired", Name: "Wired"},
		},
		AllPersonas: []config.Persona{
			{Slug: "wired", Name: "Wired", Active: true},
			{Slug: "dormant", Name: "Dormant", Active: false},
		},
	}
	snap := config.BuildSnapshot(bundle)

	all := allPersonasFromSnapshot(snap)
	if len(all) != 2 {
		t.Fatalf("allPersonasFromSnapshot len = %d, want 2 (full catalog)", len(all))
	}
	byKey := map[string]bool{}
	for _, p := range all {
		byKey[p.Key] = p.Active
	}
	if !byKey["wired"] {
		t.Fatal("wired persona Active = false, want true")
	}
	if byKey["dormant"] {
		t.Fatal("dormant persona Active = true, want false")
	}

	active := personasFromSnapshot(snap)
	if len(active) != 1 || active[0].Key != "wired" {
		t.Fatalf("personasFromSnapshot = %+v, want only the picked [wired]", active)
	}
}

// TestAllSkillsFromSnapshotCarriesActive mirrors the persona contract for the
// skill catalog.
func TestAllSkillsFromSnapshotCarriesActive(t *testing.T) {
	bundle := config.Bundle{
		Skills: []config.Skill{{Slug: "wired", Name: "Wired"}},
		AllSkills: []config.Skill{
			{Slug: "wired", Name: "Wired", Active: true},
			{Slug: "dormant", Name: "Dormant", Active: false},
		},
	}
	snap := config.BuildSnapshot(bundle)

	all := allSkillsFromSnapshot(snap)
	if len(all) != 2 {
		t.Fatalf("allSkillsFromSnapshot len = %d, want 2", len(all))
	}
	byKey := map[string]bool{}
	for _, s := range all {
		byKey[s.Key] = s.Active
	}
	if !byKey["wired"] || byKey["dormant"] {
		t.Fatalf("skill Active flags = %+v, want wired:true dormant:false", byKey)
	}
}

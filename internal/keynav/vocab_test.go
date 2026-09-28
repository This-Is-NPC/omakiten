package keynav

import (
	"reflect"
	"testing"
)

func TestTheCensusIsTheUserMap(t *testing.T) {
	if got := Default.Tops.Keys; !reflect.DeepEqual(got, []string{"shift+tab"}) {
		t.Errorf("tops = %q, want [shift+tab]", got)
	}
	if got := Default.Subs.Keys; !reflect.DeepEqual(got, []string{"/"}) {
		t.Errorf("subs = %q, want [/]", got)
	}
	if got := Default.Zones.Keys; !reflect.DeepEqual(got, []string{"tab"}) {
		t.Errorf("zones = %q, want [tab]", got)
	}
	if Default.Zones.Has("shift+tab") {
		t.Fatal("zones must not claim shift+tab; that spelling is tops")
	}
	if Default.Tops.Has("tab") {
		t.Fatal("tops must not claim tab; that spelling is zones")
	}
}

func TestKeyRingHasAndPrimary(t *testing.T) {
	r := KeyRing{Keys: []string{"a", "b"}}
	if !r.Has("a") || !r.Has("b") || r.Has("c") {
		t.Fatalf("Has drifted: %+v", r)
	}
	if r.Primary() != "a" {
		t.Fatalf("Primary = %q, want a", r.Primary())
	}
	if (KeyRing{}).Primary() != "" {
		t.Fatal("empty ring must print nothing")
	}
}

func TestCloneKeysDoesNotAliasDefault(t *testing.T) {
	got := Default.Zones.CloneKeys()
	got[0] = "mutated"
	if Default.Zones.Keys[0] != "tab" {
		t.Fatal("CloneKeys aliased Default")
	}
}

package keynav

// Vocabulary is the three navigation rings in the form the project already
// uses for a configurable key (config.NotificationDismiss): a struct with
// yaml tags and Keys []string per ring. The value lives here as a default;
// wiring it through the bundle is a later, mechanical step — this package
// does not import config and the TUI does not read m.cfg for these keys yet.
//
// Why here, not in the TUI: keynav is the keyboard model and nothing else.
// It already names the rings, depends on nothing the TUI owns, and is the
// place both the grid and the gallery may import. A private copy of the
// spellings next to each switch would be the same class of drift
// screenkit.measures closed for width.
//
// KeyRing is named apart from Ring because Ring is already the ordered
// member ids of one level (what Step walks). This type is the key
// spellings that advance a level, not the members of one.
//
// # The map (user decision)
//
//	tops    shift+tab        advance
//	        1 2 3 4 and 0    direct jump, UNCHANGED, not in this map
//	subs    /                advance
//	        ,                reverse, UNCHANGED, not in this map
//	zones   tab              advance
//
// # Zones have no reverse key
//
// shift+tab used to be "previous zone". It now advances tops, so the zone
// ring only advances. That is acceptable: the ring cycles, and the largest
// TUI zone ring is 3 members (Project meta/dashboard/activity, Task Detail
// details/subtasks/activity, Studio list/fields/inspector). Walking the
// long way costs at most two tabs. If a ring large enough to miss reverse
// appears, name its size rather than inventing a key.
type Vocabulary struct {
	Tops  KeyRing `yaml:"tops"`
	Subs  KeyRing `yaml:"subs"`
	Zones KeyRing `yaml:"zones"`
}

// KeyRing is one navigation ring's advance spellings. Keys is the list
// config will overlay; every spelling in it does the same thing (advance).
type KeyRing struct {
	Keys []string `yaml:"keys,omitempty"`
}

// Default is the census. Treat the slices as immutable — callers that
// store them must copy.
var Default = Vocabulary{
	Tops:  KeyRing{Keys: []string{"shift+tab"}},
	Subs:  KeyRing{Keys: []string{"/"}},
	Zones: KeyRing{Keys: []string{"tab"}},
}

// Has reports whether key is one of the ring's advance spellings.
func (r KeyRing) Has(key string) bool {
	for _, k := range r.Keys {
		if k == key {
			return true
		}
	}
	return false
}

// Primary is the first spelling, the one a footer prints. Empty when the
// ring has no keys.
func (r KeyRing) Primary() string {
	if len(r.Keys) == 0 {
		return ""
	}
	return r.Keys[0]
}

// CloneKeys is a copy of Keys so a caller can store the list without
// aliasing Default.
func (r KeyRing) CloneKeys() []string {
	if len(r.Keys) == 0 {
		return nil
	}
	out := make([]string, len(r.Keys))
	copy(out, r.Keys)
	return out
}

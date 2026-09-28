// Package keynav is the two-level keyboard model, and nothing else.
//
// # Why it is here and not in the TUI
//
// The rule has two implementations that cannot share code any other way. The
// grid (internal/tui/components/screengrid) drives a screen body; the
// component gallery's chrome (cmd/okt-gallery) drives its own columns, and it
// is FORBIDDEN from importing internal/tui — a tool whose navigation is built
// out of the components it inspects stops working the moment one of them
// breaks, which is precisely when it is needed.
//
// So the two had the same rule written out twice, and two copies of a rule
// drift: the layout and the navigation each grew their own idea of what a
// focus stop was, and the same body ended up offering three stops at one width
// and two at another. This package is the third place, the one both may depend
// on, and it holds no geometry, no rendering and no dependency of its own.
//
// # The rule
//
//	tab               the next SIBLING, and never a child
//	                  (the zone ring only advances; see Vocabulary)
//	f                 enter the focused thing
//	esc               come back out one level
//	↑ / ↓             move inside the focused thing, entered or not
//	j / k             the same, until the level is entered — after that they
//	                  are letters, and something being typed into owns letters
//
// `tab` refusing to descend is the point. A ring flattened over a whole tree
// reads fine two levels deep and falls apart below: `tab` stops meaning "the
// next thing beside this" and starts meaning "the next thing somewhere", so
// the presses needed to reach a zone depend on how many children its unrelated
// neighbours happen to have.
package keynav

// Keys are the spellings the model reserves. They are the app's own — task
// detail and project already print `f · esc` on their footers — so a surface
// built on this package is driven the way its users already drive one.
const (
	EnterKey = "f"
	LeaveKey = "esc"
)

// Intent is what one keystroke means in the two-level model.
type Intent int

const (
	// None is a key the model does not own; the caller decides.
	None Intent = iota
	// NextSibling and PrevSibling walk the current level.
	NextSibling
	PrevSibling
	// Enter descends into the focused thing.
	Enter
	// Leave comes back out one level.
	Leave
	// NextChild and PrevChild move INSIDE the focused thing — the next field,
	// the next item, the next row.
	NextChild
	PrevChild
)

// Resolve maps a `tea.KeyMsg.String()` spelling onto an intent.
//
// `entered` is whether the caller has gone INSIDE the focused thing. It changes
// exactly one thing: the letters. `j` and `k` move while the level is merely
// focused, because a level you are on is a level you can look through; once it
// is entered they are text, and a field being typed into owns every letter.
// The arrows are never text, so they move either way.
//
// Zone advance is read from Default.Zones rather than restated here, so a
// spelling change is one edit. PrevSibling stays in the model for a reverse
// key that does not exist in Default — the zone ring only advances.
//
// Taken as a plain string so this package stays free of bubbletea.
func Resolve(key string, entered bool) Intent {
	if Default.Zones.Has(key) {
		return NextSibling
	}
	switch key {
	case EnterKey:
		if entered {
			// Inside, `f` belongs to whatever was entered — otherwise "every key
			// reaches it" has an exception, and the exception is the key the app
			// uses most.
			return None
		}
		return Enter
	case LeaveKey:
		return Leave
	case "down":
		return NextChild
	case "up":
		return PrevChild
	case "j":
		if entered {
			return None
		}
		return NextChild
	case "k":
		if entered {
			return None
		}
		return PrevChild
	}
	return None
}

// Ring is one level's members, in the order the eye reads them.
type Ring []string

// Index is where an id sits in the ring, or -1.
func (r Ring) Index(id string) int {
	for i, member := range r {
		if member == id {
			return i
		}
	}
	return -1
}

// Step is the member `delta` places from `id`, wrapping.
//
// An id the ring does not hold steps to the FIRST member and no further: a ring
// that does not know where it is has to be repaired before it can be moved, and
// doing both in one keystroke is what makes the first press appear to skip a
// member. Reported by `seeded` so the caller can tell the repair from the move.
func (r Ring) Step(id string, delta int) (next string, seeded bool) {
	if len(r) == 0 {
		return "", false
	}
	at := r.Index(id)
	if at < 0 {
		return r[0], true
	}
	return r[((at+delta)%len(r)+len(r))%len(r)], false
}

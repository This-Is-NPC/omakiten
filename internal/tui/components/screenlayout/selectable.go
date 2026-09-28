package screenlayout

// This file is the sparse selectable mask: the answer to a body whose items are
// not all landing places.
//
// # The defect it exists for
//
// A section's items are its cursor positions — one item, one stop — and that is
// right for a list. It is wrong for a PROJECTION: Tasks > Graph draws a DAG as
// one line per node with a blank line between roots, and the blanks are items
// like any other, because they occupy rows and the window has to count them. A
// cursor stepping ±1 over that item list stops on the blanks.
//
// The screens that hit it did not ask for a mask; they kept a second cursor.
// Graph held `selectable []int` mapping a node ORDINAL to a LINE index and
// converted both ways on every keystroke — which is a private copy of "where may
// the cursor land", held beside the arranger's, and the two diverged exactly as
// every other private copy this package deletes has.
//
// # What it is
//
// [Block.Selectable] is that list, DECLARED rather than kept: the ascending item
// indices the cursor may occupy. It changes only where the cursor may LAND. The
// items, the heights, the window, the offset and the hint counts are all
// untouched — a masked-out item is still painted, still measured, still scrolled
// past. That is the whole point: the blank between two roots must stay visible,
// it must simply not be selectable.
//
// # Why nil is the zero value and a strict no-op
//
// Twenty of the twenty-one screens have no mask, and a Block literal that says
// nothing about it must keep stepping ±1. So every function here is the identity
// on an empty mask, and the switch in [cursorTarget] is a single length test.
// Pinned by TestAnAbsentMaskChangesNothing plus the whole pre-existing test file
// set, which was not modified when this landed.
//
// # Why the mask is normalised rather than trusted
//
// Same rule [measureItems] applies to Block.Heights: a section's statement about
// itself is checked, never believed. A mask is normalised against the items that
// actually exist, so an index a refresh left dangling cannot park a cursor
// outside the list, and an entry out of ascending order cannot make the stepping
// go backwards on `j`. A mask that normalises to nothing is a mask that says
// nothing, which is the no-op above.

// normalizeMask is the declared mask reduced to strictly ascending indices that
// address items that exist. It returns nil for "no mask", which is what every
// helper below treats as the identity.
func normalizeMask(mask []int, count int) []int {
	if len(mask) == 0 || count <= 0 {
		return nil
	}
	out := make([]int, 0, len(mask))
	last := -1
	for _, index := range mask {
		if index <= last || index < 0 || index >= count {
			continue
		}
		out = append(out, index)
		last = index
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// maskFloor is the greatest member at or below cursor, or the first member when
// the cursor sits below all of them.
//
// It is the general snap — the one [renderSection] and [reclamp] settle a cursor
// with — because it is what a cursor that has been clamped off the end of a
// shrunken list needs: the nearest landing place BACK along the list, never a
// jump forward past content the user was reading.
func maskFloor(cursor int, mask []int) int {
	if len(mask) == 0 || cursor < 0 {
		return cursor
	}
	best := mask[0]
	for _, index := range mask {
		if index > cursor {
			break
		}
		best = index
	}
	return best
}

// maskPrev is the greatest member strictly below cursor, or the first member
// when there is none — so `k` on the first selectable item stays put, exactly as
// an unmasked cursor clamped at 0 does.
func maskPrev(cursor int, mask []int) int {
	if len(mask) == 0 {
		return cursor
	}
	best := mask[0]
	for _, index := range mask {
		if index >= cursor {
			break
		}
		best = index
	}
	return best
}

// maskNext is the smallest member strictly above cursor, or the last member when
// there is none — so `j` on the last selectable item stays put.
func maskNext(cursor int, mask []int) int {
	if len(mask) == 0 {
		return cursor
	}
	for _, index := range mask {
		if index > cursor {
			return index
		}
	}
	return mask[len(mask)-1]
}

// maskPos is the position IN THE MASK of the member the cursor is on — or, for
// a cursor that is somehow off the mask, of the member below it. Zero when the
// cursor sits below every member.
func maskPos(cursor int, mask []int) int {
	at := 0
	for i, index := range mask {
		if index > cursor {
			break
		}
		at = i
	}
	return at
}

// maskTarget is [cursorTarget] for a section that declared a mask.
//
// Every action here is the unmasked action with one substitution: the cursor
// moves over MEMBERS where it used to move over items. Single steps go to the
// next and previous member, the ends go to the ends of the mask, and the page
// keys move by the same [pageStep] the unmasked path computes — the same
// integer, from the same measured heights and the same item viewport — counted
// in members instead of in items.
//
// # Why a page counts members
//
// Because that is what a page has always counted. Without a mask, an item IS a
// cursor position, so "half a viewport of items" and "half a viewport of cursor
// stops" are the same sentence; the mask is what pulls them apart, and the one
// that survives is the one about cursor stops. A page key's job is to advance
// the SELECTION by about a screenful and let the window follow it, which is the
// behaviour every list on this tree already has and the behaviour Graph's
// recorded fixtures pin — its two blank root separators are exactly the items a
// page counted in ITEMS would spend a step on and a page counted in members does
// not. All twelve of Graph's view goldens are byte-identical under this rule and
// two of them (tree-scrolled at 120x40 and at 200x50) move by one line under the
// other one.
//
// The property this gives up is that a page never travels more rows than a page:
// a mask whose members are ten rows apart advances ten pages' worth of rows on
// one press. Nothing in the tree declares a mask that sparse — Graph's members
// are 52 of its 54 lines — and the alternative gives up the selection-relative
// step instead, which is the one a user's fingers know. If a genuinely sparse
// mask ever lands, the fix is a row-budgeted walk over the members, not a
// silent switch of unit.
func maskTarget(r measured, action Action) int {
	mask := r.selectable
	at := maskPos(r.cursor, mask)
	switch action {
	case ActionUp:
		return maskPrev(r.cursor, mask)
	case ActionDown:
		return maskNext(r.cursor, mask)
	case ActionPageUp:
		return mask[max(0, at-pageStep(r))]
	case ActionPageDown:
		return mask[min(len(mask)-1, at+pageStep(r))]
	case ActionFirst:
		return mask[0]
	case ActionLast:
		return mask[len(mask)-1]
	}
	return r.cursor
}

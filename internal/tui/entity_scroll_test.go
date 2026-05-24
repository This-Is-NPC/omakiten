package tui

import (
	"fmt"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TestEntityGridScrollIsRowIndexNotCardIndex pins the W11-B-4
// migration: the per-kind cardlist.Model owns the scroll field as
// a ROW INDEX, not a card index. Walks the cursor through every
// card position and asserts the cardlist's Scroll() stays in
// row-index range and the cursor row never sits above the scroll.
func TestEntityGridScrollIsRowIndexNotCardIndex(t *testing.T) {
	const cardCount = 30
	tags := make([]domain.Tag, cardCount)
	for i := range tags {
		tags[i] = domain.Tag{ID: int64(i + 1), Name: fmt.Sprintf("tag-%02d", i), Label: fmt.Sprintf("tag-%02d", i), UsageCount: 1}
	}

	m := Model{
		styles:        newStyles(config.Theme{}),
		width:         200,
		height:        40,
		tags:          tags,
		entityKind:    entityKindTag,
		entityCursors: map[entityKind]int{entityKindTag: 0},
	}

	cols := entityGridCols(m.entityCellContentWidth())
	if cols < 1 {
		t.Fatalf("entityGridCols returned %d, want >=1", cols)
	}
	numRows := (cardCount + cols - 1) / cols

	for cursor := 0; cursor < cardCount; cursor++ {
		m.entityCursors[entityKindTag] = cursor
		m.syncFocusedEntityScroll()
		list, ok := m.entityLists[entityKindTag]
		if !ok {
			t.Fatalf("cursor %d: entityLists missing entry", cursor)
		}
		scroll := list.Scroll()
		listCursor := list.Cursor()
		cursorRow := cursor / cols
		if scroll < 0 || scroll >= numRows {
			t.Fatalf("cursor %d: scroll=%d out of row-index range [0,%d)", cursor, scroll, numRows)
		}
		if listCursor != cursorRow {
			t.Fatalf("cursor %d: list.Cursor=%d, want row=%d", cursor, listCursor, cursorRow)
		}
		if cursorRow < scroll {
			t.Fatalf("cursor %d: cursorRow=%d above scroll=%d (cursor row scrolled off the top)", cursor, cursorRow, scroll)
		}
	}
}

// TestEntityGridEmptyKindDropsListEntry pins the housekeeping
// invariant: a kind with zero entities must drop its entityLists
// entry so the map does not accumulate ghost entries.
func TestEntityGridEmptyKindDropsListEntry(t *testing.T) {
	tags := []domain.Tag{{ID: 1, Name: "x", Label: "x", UsageCount: 1}}
	m := Model{
		styles:        newStyles(config.Theme{}),
		width:         200,
		height:        40,
		tags:          tags,
		entityKind:    entityKindTag,
		entityCursors: map[entityKind]int{entityKindTag: 0},
	}
	m.syncFocusedEntityScroll()
	if _, ok := m.entityLists[entityKindTag]; !ok {
		t.Fatalf("setup: expected entityLists[Tag] after sync with 1 tag")
	}
	m.tags = nil
	m.syncFocusedEntityScroll()
	if _, ok := m.entityLists[entityKindTag]; ok {
		t.Fatalf("empty tag list left stale entityLists entry")
	}
}

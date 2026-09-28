package screengrid

import (
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// ListInspector is the `lista | inspector` body: two columns that sit side by
// side when the terminal can afford both minimums, and stack past that
// breakpoint.
//
// `list` is the left cell, typically [Cell] of [screenlayout.List]. `inspector`
// is the right column, typically [InspectorColumn]. The gap and the breakpoint
// come from the vocabulary; a screen that is this shape does not redeclare them.
func ListInspector(root screenlayout.ID, list, inspector Node) Node {
	return Cols(screenlayout.Spec{ID: root, ColumnGap: screenkit.ZoneGap}, list, inspector)
}

// InspectorColumn is the right-hand column of `lista | inspector`: the field
// table over the detail box, as a Rows the layout dissolves and the keyboard
// walks straight through.
//
// A nil body is a zone this row does not have — a Workflow bucket has no
// detail box, an open transition has no field table — and it is left out of
// the tree rather than declared and painted empty. The ring is the zones on
// screen, so a zone that is not on screen must not be a stop.
//
// Lifted from the four Studio sub-screens. The row split is the one those
// screens already had: fields declare a ceiling and fill toward it first;
// the detail box takes everything left over.
func InspectorColumn(ids screenlayout.InspectorIDs, fields, detail Node, hasFields, hasDetail bool) Node {
	spec := screenlayout.Inspector(ids.Inspector)
	switch {
	case hasFields && hasDetail:
		return Rows(spec, fields, detail)
	case hasDetail:
		return Rows(spec, detail)
	default:
		return Rows(spec, fields)
	}
}

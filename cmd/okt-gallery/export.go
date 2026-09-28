package main

import (
	"encoding/json"
	"io"
)

// The catalog is the only record of what the gallery shows, and until now it
// existed as Go source alone. Anything that wanted to ask it a question — which
// components have an empty-state scenario, which entries declare a floor — had
// to parse catalog.go, and a regex over Go source cannot see a scenario list
// that a function GENERATES: shellScenarios() builds 24 rows from 8 shapes, and
// every textual census of this file has reported the shell as having none.
//
// `okt-gallery -json` makes the catalog data. The generator runs, so generated
// scenarios are in the output like any other, and a question about coverage
// becomes a query instead of a parse.

// exportedCatalog is the whole catalog as data.
type exportedCatalog struct {
	Entries []exportedEntry `json:"entries"`
}

// exportedEntry is one inspectable subject. Kind separates the two halves of
// the catalog: a component entry names the package it lives in, a screen entry
// names the screenhost id it mounts.
type exportedEntry struct {
	Name      string             `json:"name"`
	Kind      string             `json:"kind"`
	Package   string             `json:"package,omitempty"`
	Screen    string             `json:"screen,omitempty"`
	Title     string             `json:"title"`
	MinWidth  int                `json:"minWidth"`
	MinHeight int                `json:"minHeight"`
	Scenarios []exportedScenario `json:"scenarios"`
}

// exportedScenario carries BOTH geometries on purpose.
//
// Declared is what the scenario literal wrote; Width/Height are what it
// actually paints at, which is the entry's floor when the scenario left an axis
// at zero. A consumer asking "does this fit 80 columns" needs the resolved
// pair, and a consumer asking "did anyone declare this" needs the raw one.
// Emitting only one of them would make one of those two questions unanswerable.
type exportedScenario struct {
	Name           string            `json:"name"`
	Note           string            `json:"note,omitempty"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	DeclaredWidth  int               `json:"declaredWidth"`
	DeclaredHeight int               `json:"declaredHeight"`
	Props          map[string]string `json:"props,omitempty"`
}

// exportCatalog writes the catalog as indented JSON.
//
// It walks entries() — the same slice the gallery itself renders from — so the
// export cannot drift from what the tool shows. There is no second list to keep
// in step.
func exportCatalog(w io.Writer) error {
	out := exportedCatalog{Entries: make([]exportedEntry, 0, len(entries()))}
	for _, e := range entries() {
		out.Entries = append(out.Entries, exportEntry(e))
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func exportEntry(e entry) exportedEntry {
	scenarios := make([]exportedScenario, 0, len(e.scenarios))
	for _, s := range e.scenarios {
		width, height := scenarioFrameSize(e, s)
		scenarios = append(scenarios, exportedScenario{
			Name: s.name, Note: s.note,
			Width: width, Height: height,
			DeclaredWidth: s.width, DeclaredHeight: s.height,
			Props: s.props,
		})
	}
	return exportedEntry{
		Name: e.name, Kind: entryKind(e), Package: e.pkg, Screen: e.screen,
		Title: e.title, MinWidth: e.minWidth, MinHeight: e.minHeight,
		Scenarios: scenarios,
	}
}

// entryKind reports which half of the catalog an entry belongs to. The two are
// distinguished by which field is set, which is the same test the coverage gate
// in internal/arch applies.
func entryKind(e entry) string {
	if e.screen != "" {
		return "screen"
	}
	return "component"
}

// scenarioFrameSize resolves a scenario's geometry against its entry's floor: a
// scenario that omits an axis paints at the entry minimum on that axis.
//
// It lives here rather than in the gate's test file because the fit gate and
// the export must agree on what "the size this scenario paints at" means. Two
// copies of this would let the JSON report a geometry the gate never checked.
func scenarioFrameSize(e entry, s scenario) (int, int) {
	width, height := s.width, s.height
	if width <= 0 {
		width = e.minWidth
	}
	if height <= 0 {
		height = e.minHeight
	}
	return width, height
}

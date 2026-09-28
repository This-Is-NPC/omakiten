package studio

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// Changing the lista|inspector breakpoint used to be six constants in five
// files: a *ListMinWidth, *InspectorMinWidth and *ColumnGap on each of the
// four Studio sub-screens, plus the shared inspector helper that threaded
// the same 40/40/2 through. One edit never moved all four.
//
// The four now take their body from ListInspector, List and InspectorColumn,
// which read ListFloor, InspectorFloor and ZoneGap. This gate reads the
// SOURCE of those four files, not the arrangement they produce.
//
// A layout probe that compared MinWidth to ListFloor would pass a local
// commandsListMinWidth of 40 at today's 40/40/2 and still be the six-edit
// form — that is why the gate reads the source, not the arrangement. The
// shape only shows as a defect once the vocabulary moves, which is too
// late for a gate to be worth having. Seeded copies of the pre-migration
// form are rejected even when the integer equals today's ListFloor.

var listInspectorScreens = []string{
	"commands.go",
	"workflow.go",
	"personas.go",
	"hooks.go",
}

var widthMeasureFields = []string{
	"MinWidth",
	"MaxWidth",
	"WidthPercent",
	"ColumnGap",
}

func TestListInspectorMeasureMovesAllFourStudioScreensTogether(t *testing.T) {
	t.Parallel()
	assertListInspectorConstructorsBakeTheVocabulary(t)

	if len(listInspectorScreens) != 4 {
		t.Fatalf("listInspectorScreens = %d (%v), want the four lista|inspector bodies",
			len(listInspectorScreens), listInspectorScreens)
	}
	for _, name := range listInspectorScreens {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		verdict := inspectListInspectorMeasure(name, src)
		if verdict.parseErr != nil {
			t.Fatalf("parse %s: %v", name, verdict.parseErr)
		}
		reportListInspectorMeasure(t, name, verdict)
	}
}

func assertListInspectorConstructorsBakeTheVocabulary(t *testing.T) {
	t.Helper()

	list := screenlayout.List("list")
	if list.MinWidth != screenkit.ListFloor {
		t.Errorf("List().MinWidth = %d, want ListFloor %d", list.MinWidth, screenkit.ListFloor)
	}
	if list.ColumnGap != screenkit.ZoneGap {
		t.Errorf("List().ColumnGap = %d, want ZoneGap %d", list.ColumnGap, screenkit.ZoneGap)
	}

	inspector := screenlayout.Inspector("inspector")
	if inspector.MinWidth != screenkit.InspectorFloor {
		t.Errorf("Inspector().MinWidth = %d, want InspectorFloor %d", inspector.MinWidth, screenkit.InspectorFloor)
	}
	if inspector.ColumnGap != screenkit.ZoneGap {
		t.Errorf("Inspector().ColumnGap = %d, want ZoneGap %d", inspector.ColumnGap, screenkit.ZoneGap)
	}

	ids := screenlayout.InspectorIDs{Inspector: "inspector", Fields: "fields", Detail: "detail"}
	root := screengrid.ListInspector("root",
		screengrid.Cell(list, listInspectorMeasureEmptyBody),
		screengrid.InspectorColumn(ids,
			screengrid.Cell(ids.FieldsSpec(0), listInspectorMeasureEmptyBody),
			screengrid.Cell(ids.DetailSpec(false, 0), listInspectorMeasureEmptyBody),
			true, true))
	if root.Spec.ColumnGap != screenkit.ZoneGap {
		t.Errorf("ListInspector().ColumnGap = %d, want ZoneGap %d", root.Spec.ColumnGap, screenkit.ZoneGap)
	}
}

func listInspectorMeasureEmptyBody(screenlayout.Canvas) screenlayout.Block {
	return screenlayout.Block{}
}

// The gate has to fail on the states it exists to prevent, or it is a test that
// passes by matching nothing. The first five seeds use today's census integers
// (40, 2) and must still fail: that is the copy a layout probe would bless.
func TestListInspectorMeasureGateRejectsBypasses(t *testing.T) {
	t.Parallel()

	honest := `package studio

func root() {
	screengrid.ListInspector(id,
		screengrid.Cell(screenlayout.List(listID), body),
		screengrid.InspectorColumn(ids, fields, detail, true, true))
}
`
	cases := []struct {
		name      string
		src       string
		wantLocal bool
		wantArch  bool
		wantPass  bool
	}{
		{
			name:      "a local const of today's ListFloor",
			wantLocal: true,
			src: `package studio

const commandsListMinWidth = 40

func root() {
	screengrid.ListInspector(id,
		screengrid.Cell(screenlayout.List(listID), body),
		screengrid.InspectorColumn(ids, fields, detail, true, true))
}
`,
		},
		{
			name:      "a local const of today's ZoneGap",
			wantLocal: true,
			src: `package studio

const zoneGap = 2

func root() {
	screengrid.ListInspector(id,
		screengrid.Cell(screenlayout.List(listID), body),
		screengrid.InspectorColumn(ids, fields, detail, true, true))
}
`,
		},
		{
			name:      "a literal on a Spec width key",
			wantLocal: true,
			src: `package studio

func root() {
	screengrid.ListInspector(id,
		screengrid.Cell(screenlayout.List(listID), body),
		screengrid.InspectorColumn(ids, fields, detail, true, true))
	_ = screenlayout.Spec{MinWidth: 40, ColumnGap: 2}
}
`,
		},
		{
			name:      "a field write of today's ListFloor",
			wantLocal: true,
			src: `package studio

func root() {
	spec := screenlayout.List(listID)
	spec.MinWidth = 40
	screengrid.ListInspector(id,
		screengrid.Cell(spec, body),
		screengrid.InspectorColumn(ids, fields, detail, true, true))
}
`,
		},
		{
			name:     "a body that calls Cols and never the constructors",
			wantArch: true,
			src: `package studio

func root() {
	screengrid.Cols(screenlayout.Spec{ID: id}, list, inspector)
}
`,
		},
		{
			name:     "the honest ListInspector + Cell(List) + InspectorColumn form",
			wantPass: true,
			src:      honest,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertListInspectorMeasureCase(t, testCase.name, testCase.src, testCase.wantLocal, testCase.wantArch, testCase.wantPass)
		})
	}
}

func assertListInspectorMeasureCase(t *testing.T, name, src string, wantLocal, wantArch, wantPass bool) {
	t.Helper()
	verdict := inspectListInspectorMeasure("commands.go", []byte(src))
	if verdict.parseErr != nil {
		t.Fatalf("seed %q did not parse: %v", name, verdict.parseErr)
	}
	probe := &testing.T{}
	reportListInspectorMeasure(probe, "commands.go", verdict)
	if wantPass {
		if probe.Failed() {
			t.Errorf("the gate rejected the honest form — local %v, missing archetype %v", verdict.local, !verdict.archetype)
		}
		return
	}
	if !probe.Failed() {
		t.Errorf("the gate accepted %q — it must fail", name)
	}
	if wantLocal && len(verdict.local) == 0 {
		t.Errorf("the gate must accuse a local measure for %q", name)
	}
	if wantArch && verdict.archetype {
		t.Errorf("the gate must accuse a missing archetype for %q", name)
	}
}

type listInspectorMeasureVerdict struct {
	local     []string
	archetype bool
	parseErr  error
}

func inspectListInspectorMeasure(filename string, src []byte) listInspectorMeasureVerdict {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return listInspectorMeasureVerdict{parseErr: err}
	}
	verdict := listInspectorMeasureVerdict{}
	called := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.GenDecl:
			verdict.local = append(verdict.local, localWidthMeasureConsts(fset, filename, n)...)
		case *ast.CompositeLit:
			verdict.local = append(verdict.local, localWidthMeasureKeys(fset, filename, n)...)
		case *ast.AssignStmt:
			verdict.local = append(verdict.local, localWidthMeasureWrites(fset, filename, n)...)
		case *ast.CallExpr:
			if name, ok := listInspectorConstructorCall(n); ok {
				called[name] = true
			}
		}
		return true
	})
	verdict.archetype = called["ListInspector"] && called["List"] && called["InspectorColumn"]
	return verdict
}

func reportListInspectorMeasure(t *testing.T, filename string, verdict listInspectorMeasureVerdict) {
	t.Helper()
	if verdict.parseErr != nil {
		t.Errorf("parse %s: %v", filename, verdict.parseErr)
		return
	}
	for _, msg := range verdict.local {
		t.Errorf("%s", msg)
	}
	if !verdict.archetype {
		t.Errorf("%s does not take its body from ListInspector, List and InspectorColumn — a screengrid.Cols of local Specs is the six-edit form", filename)
	}
}

func localWidthMeasureConsts(fset *token.FileSet, filename string, decl *ast.GenDecl) []string {
	if decl.Tok != token.CONST {
		return nil
	}
	var found []string
	for _, spec := range decl.Specs {
		found = append(found, localWidthMeasureSpec(fset, filename, spec)...)
	}
	return found
}

func localWidthMeasureSpec(fset *token.FileSet, filename string, spec ast.Spec) []string {
	valueSpec, ok := spec.(*ast.ValueSpec)
	if !ok {
		return nil
	}
	var found []string
	for i, name := range valueSpec.Names {
		if i >= len(valueSpec.Values) {
			continue
		}
		value := valueSpec.Values[i]
		if !isIntLiteral(value) {
			continue
		}
		if !isLocalWidthMeasureName(name.Name) && !isListInspectorFloorLiteral(value) {
			continue
		}
		found = append(found, localMeasureMessage(fset, filename, name, name.Name+" = "+nodeText(fset, value)))
	}
	return found
}

func localWidthMeasureKeys(fset *token.FileSet, filename string, lit *ast.CompositeLit) []string {
	var found []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || !isWidthMeasureField(key.Name) || !isIntLiteral(kv.Value) {
			continue
		}
		found = append(found, localMeasureMessage(fset, filename, kv, nodeText(fset, kv)))
	}
	return found
}

func localWidthMeasureWrites(fset *token.FileSet, filename string, stmt *ast.AssignStmt) []string {
	if stmt.Tok != token.ASSIGN {
		return nil
	}
	var found []string
	for i, lhs := range stmt.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || !isWidthMeasureField(sel.Sel.Name) || i >= len(stmt.Rhs) || !isIntLiteral(stmt.Rhs[i]) {
			continue
		}
		found = append(found, localMeasureMessage(fset, filename, lhs, nodeText(fset, lhs)+" = "+nodeText(fset, stmt.Rhs[i])))
	}
	return found
}

func localMeasureMessage(fset *token.FileSet, filename string, node ast.Node, expr string) string {
	line := fset.Position(node.Pos()).Line
	return filename + ":" + strconv.Itoa(line) + ": declares the list|inspector measure locally (" + expr + "); take it from List, Inspector and ListInspector"
}

func listInspectorConstructorCall(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	switch sel.Sel.Name {
	case "ListInspector", "InspectorColumn":
		return sel.Sel.Name, true
	case "List":
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "screenlayout" {
			return "List", true
		}
	}
	return "", false
}

func isWidthMeasureField(name string) bool {
	for _, field := range widthMeasureFields {
		if field == name {
			return true
		}
	}
	return false
}

func isLocalWidthMeasureName(name string) bool {
	lower := strings.ToLower(name)
	for _, field := range widthMeasureFields {
		if strings.Contains(lower, strings.ToLower(field)) {
			return true
		}
	}
	for _, needle := range []string{"zonegap", "listfloor", "inspectorfloor"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func isListInspectorFloorLiteral(expr ast.Expr) bool {
	n, ok := intLiteralValue(expr)
	return ok && (n == screenkit.ListFloor || n == screenkit.InspectorFloor)
}

func isIntLiteral(expr ast.Expr) bool {
	_, ok := intLiteralValue(expr)
	return ok
}

func intLiteralValue(expr ast.Expr) (int, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	n, err := strconv.Atoi(lit.Value)
	if err != nil {
		return 0, false
	}
	return n, true
}

func nodeText(fset *token.FileSet, node ast.Node) string {
	var buf strings.Builder
	if err := format.Node(&buf, fset, node); err != nil {
		return "<expr>"
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

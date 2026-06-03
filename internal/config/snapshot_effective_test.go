package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestEffectiveTuples_OrderAndCoercion pins the deterministic ordering
// contract (Settings struct field order at the section level, dot-path
// ascending within a section) and the scalar-coercion shape required by
// the TUI viewer.
func TestEffectiveTuples_OrderAndCoercion(t *testing.T) {
	bundle := newTwoBucketBundle("alpha", "beta")
	cachePrompts := true
	includeWF := false
	bundle.Config = Settings{
		Output: OutputSettings{
			JSONMinified: false,
			OmitEmpty:    true,
		},
		Workflow: WorkflowSettings{Active: "test"},
		Theme: ThemeSettings{
			Active: "omacon",
		},
		TemplateDefaults: []string{"feature", "bug"},
		MCP: MCPSettings{
			RecentCommentLimit:        5,
			MaxCommentChars:           300,
			IncludeWorkflowInContinue: &includeWF,
			CachePrompts:              &cachePrompts,
			NextWorkLimit:             4,
			SimilarTaskLimit:          6,
		},
		TagSynonyms: map[string]string{
			"bugfix": "bug",
			"feat":   "feature",
		},
		Priorities: []PriorityDefinition{
			{ID: 1, Value: "low"},
			{ID: 2, Value: "normal", Default: true},
		},
		Languages: LanguageSettings{
			CLI: "en",
			TUI: "pt-br",
		},
	}

	snap := BuildSnapshot(bundle)
	tuples := snap.EffectiveTuples()
	if len(tuples) == 0 {
		t.Fatal("EffectiveTuples returned no rows for a populated Settings")
	}

	// Order leg 1: top-level section order matches the Settings struct
	// field order (Output → Context → Workflow → Theme → …). Pull the
	// first occurrence of each section and compare against the prefix
	// of the field-name list.
	sectionOrder := []string{}
	seen := map[string]bool{}
	for _, tup := range tuples {
		if seen[tup.Section] {
			continue
		}
		seen[tup.Section] = true
		sectionOrder = append(sectionOrder, tup.Section)
	}
	// Settings field order (yaml tags lowercased / under_scored). Only
	// the sections actually populated above appear in the output.
	wantPrefix := []string{
		"output",
		"workflow",
		"theme",
		"template_defaults",
		"mcp",
		"tag_synonyms",
		"priorities",
		"languages",
	}
	if diff := cmp.Diff(wantPrefix, sectionOrder); diff != "" {
		t.Fatalf("section order mismatch (-want +got):\n%s", diff)
	}

	// Order leg 2: within a section, dot-path keys ascend.
	for sectionStart := 0; sectionStart < len(tuples); {
		section := tuples[sectionStart].Section
		end := sectionStart
		for end < len(tuples) && tuples[end].Section == section {
			end++
		}
		group := tuples[sectionStart:end]
		keys := make([]string, len(group))
		for i, g := range group {
			keys[i] = g.Key
		}
		if !sort.StringsAreSorted(keys) {
			t.Errorf("section %q keys not ascending: %v", section, keys)
		}
		sectionStart = end
	}

	// Coercion: booleans, numerics, strings carry canonical literals.
	want := map[string]string{
		"output.json_minified":               "false",
		"output.omit_empty":                  "true",
		"theme.active":                       "omacon",
		"mcp.recent_comment_limit":           "5",
		"mcp.cache_prompts":                  "true",
		"mcp.include_workflow_in_continue":   "false",
		"priorities[0].id":                   "1",
		"priorities[0].value":                "low",
		"priorities[1].default":              "true",
		"priorities[1].value":                "normal",
		"tag_synonyms.bugfix":                "bug",
		"tag_synonyms.feat":                  "feature",
		"template_defaults[0]":               "feature",
		"template_defaults[1]":               "bug",
		"languages.cli":                      "en",
		"languages.tui":                      "pt-br",
	}
	got := map[string]string{}
	for _, tup := range tuples {
		got[joinPath(tup.Section, tup.Key)] = tup.Value
	}
	for path, val := range want {
		gotVal, ok := got[path]
		if !ok {
			t.Errorf("expected path %q present in tuples; got none", path)
			continue
		}
		if gotVal != val {
			t.Errorf("path %q value: got %q want %q", path, gotVal, val)
		}
	}
}

// TestEffectiveTuples_Completeness asserts no silent drops: every scalar
// reachable from Settings via an independent reflect walk shows up in
// the accessor output. This is the regression guard for "I added a new
// Settings field and forgot to expose it" — the reflect walk does not
// know about EffectiveTuples implementation details, so if the two
// disagree the accessor is dropping a field.
func TestEffectiveTuples_Completeness(t *testing.T) {
	bundle := newTwoBucketBundle("alpha", "beta")
	cachePrompts := true
	includeWF := true
	bundle.Config = Settings{
		Output: OutputSettings{JSONMinified: true, OmitEmpty: true},
		Workflow: WorkflowSettings{
			Active: "test",
		},
		Theme: ThemeSettings{Active: "omacon"},
		MCP: MCPSettings{
			RecentCommentLimit:        10,
			MaxCommentChars:           500,
			IncludeWorkflowInContinue: &includeWF,
			CachePrompts:              &cachePrompts,
			NextWorkLimit:             8,
			SimilarTaskLimit:          9,
		},
		Priorities: []PriorityDefinition{{ID: 1, Value: "low"}},
		Severities: []SeverityDefinition{{ID: 1, Value: "minor"}},
	}

	snap := BuildSnapshot(bundle)
	gotPaths := snap.effectiveTuplePathsForTest()
	gotSet := map[string]bool{}
	for _, p := range gotPaths {
		gotSet[p] = true
	}

	// Walk Settings via reflect; collect every scalar leaf path the
	// accessor *should* visit. Skips fields tagged `yaml:"-"`, mirrors
	// the empty-map / empty-slice rendering rules, and respects
	// `omitempty` so zero-valued optional blocks don't show up as
	// expected-but-missing.
	wantPaths := walkScalarPaths(reflect.ValueOf(bundle.Config), "", "")

	missing := []string{}
	for _, p := range wantPaths {
		if !gotSet[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("EffectiveTuples dropped %d scalar(s) reachable from Settings:\n  %s\n\ngot paths:\n  %s",
			len(missing),
			strings.Join(missing, "\n  "),
			strings.Join(gotPaths, "\n  "),
		)
	}
}

// TestEffectiveTuples_NilReceiver pins the nil-safe contract; the TUI
// viewer renders an empty list (no panic) when the snapshot pointer
// hasn't materialised yet during boot.
func TestEffectiveTuples_NilReceiver(t *testing.T) {
	var snap *Snapshot
	if got := snap.EffectiveTuples(); got != nil {
		t.Fatalf("nil receiver: want nil tuples, got %v", got)
	}
	if got := snap.EffectiveSectionKeys(); got != nil {
		t.Fatalf("nil receiver: want nil sections, got %v", got)
	}
}

// TestEffectiveTuples_SourceNonEmptyOnEveryRow pins the per-row
// contract: once the loader-merge instrumentation lands, every tuple
// reports a non-empty Source from the allowed set {default, project,
// env}. Bundles built via newTwoBucketBundle bypass LoadBundle so the
// Sources map is nil; the snapshot's SourceFor falls back to
// SourceDefault, which keeps the viewer column populated rather than
// rendering empty cells.
func TestEffectiveTuples_SourceNonEmptyOnEveryRow(t *testing.T) {
	bundle := newTwoBucketBundle("alpha", "beta")
	bundle.Config.Output.JSONMinified = true
	bundle.Config.Theme = ThemeSettings{Active: "omacon"}
	bundle.Config.MCP = MCPSettings{
		RecentCommentLimit:        3,
		MaxCommentChars:           200,
		IncludeWorkflowInContinue: boolPtr(true),
		CachePrompts:              boolPtr(false),
		NextWorkLimit:             2,
		SimilarTaskLimit:          2,
	}
	allowed := map[string]bool{
		SourceDefault: true,
		SourceProject: true,
		SourceEnv:     true,
	}
	snap := BuildSnapshot(bundle)
	tuples := snap.EffectiveTuples()
	if len(tuples) == 0 {
		t.Fatal("expected at least one tuple from populated Settings")
	}
	for _, tup := range tuples {
		if tup.Source == "" {
			t.Errorf("tuple %s.%s: Source is empty (must be one of %v)",
				tup.Section, tup.Key, sortedAllowed(allowed))
			continue
		}
		if !allowed[tup.Source] {
			t.Errorf("tuple %s.%s: Source=%q not in %v",
				tup.Section, tup.Key, tup.Source, sortedAllowed(allowed))
		}
	}
}

// TestEffectiveTuples_SourceLayersThreadThrough exercises the snapshot
// threading end-to-end against all three labels. The fixture seeds
// Bundle.Sources directly (LoadBundle is not required to enter this
// branch — production callers populate the same map there) and the
// accessor surfaces the seeded label on each matching tuple. The
// production loader populates SourceDefault / SourceProject from the
// kit baseline diff and SourceEnv from ApplyEnvOverlay; this test
// covers the downstream wiring so the renderer column is unblocked
// without depending on the on-disk loader path.
func TestEffectiveTuples_SourceLayersThreadThrough(t *testing.T) {
	bundle := newTwoBucketBundle("alpha", "beta")
	bundle.Config.Output = OutputSettings{JSONMinified: true, OmitEmpty: true}
	bundle.Config.Theme = ThemeSettings{Active: "omacon"}
	bundle.Config.MCP = MCPSettings{
		RecentCommentLimit:        9,
		MaxCommentChars:           300,
		IncludeWorkflowInContinue: boolPtr(false),
		CachePrompts:              boolPtr(true),
		NextWorkLimit:             4,
		SimilarTaskLimit:          4,
	}
	bundle.Sources = map[string]string{
		"output.json_minified":     SourceProject,
		"output.omit_empty":        SourceDefault,
		"theme.active":             SourceEnv,
		"mcp.recent_comment_limit": SourceProject,
		"mcp.cache_prompts":        SourceDefault,
	}

	snap := BuildSnapshot(bundle)
	got := map[string]string{}
	for _, tup := range snap.EffectiveTuples() {
		got[joinPath(tup.Section, tup.Key)] = tup.Source
	}

	cases := map[string]string{
		"output.json_minified":     SourceProject,
		"output.omit_empty":        SourceDefault,
		"theme.active":             SourceEnv,
		"mcp.recent_comment_limit": SourceProject,
		"mcp.cache_prompts":        SourceDefault,
	}
	for path, want := range cases {
		if got[path] != want {
			t.Errorf("path %q: got Source=%q, want %q", path, got[path], want)
		}
	}

	// Unseeded paths fall through to SourceDefault — proves the snapshot
	// accessor's fallback covers leaves the loader did not record (e.g.
	// future-added Settings fields seen by the accessor before the
	// loader's source-tracker is taught about them).
	if got["mcp.max_comment_chars"] != SourceDefault {
		t.Errorf("unseeded path mcp.max_comment_chars: got %q, want %q (fallback)",
			got["mcp.max_comment_chars"], SourceDefault)
	}
}

// TestSnapshot_SourceFor_NilSafeAndFallback pins the receiver-level
// contract for the SourceFor accessor: nil receiver and missing path
// both return SourceDefault so call sites never need a nil check before
// rendering the column.
func TestSnapshot_SourceFor_NilSafeAndFallback(t *testing.T) {
	var nilSnap *Snapshot
	if got := nilSnap.SourceFor("any.path"); got != SourceDefault {
		t.Fatalf("nil receiver: got %q want %q", got, SourceDefault)
	}
	bundle := newTwoBucketBundle("alpha", "beta")
	bundle.Sources = map[string]string{"theme.active": SourceProject}
	snap := BuildSnapshot(bundle)
	if got := snap.SourceFor("theme.active"); got != SourceProject {
		t.Fatalf("seeded path: got %q want %q", got, SourceProject)
	}
	if got := snap.SourceFor("not.in.map"); got != SourceDefault {
		t.Fatalf("missing path: got %q want %q", got, SourceDefault)
	}
}

func sortedAllowed(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// walkScalarPaths is the test-only reflect counterpart to flattenNode.
// It enumerates the dot-paths an accessor must visit if it is honest
// about every scalar reachable from the input value. The walker respects
// `yaml:"-"` to skip hidden fields, applies omitempty for zero values,
// and uses the same `[N]` index convention as flattenNode.
//
// The walker intentionally does NOT call into accessor code — its
// purpose is to detect divergence, so reusing the same flattener would
// blind the test to drift.
func walkScalarPaths(v reflect.Value, sectionPrefix, keyPrefix string) []string {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return walkScalarPaths(v.Elem(), sectionPrefix, keyPrefix)
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return walkScalarPaths(v.Elem(), sectionPrefix, keyPrefix)
	case reflect.Struct:
		out := []string{}
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			tag, omitempty := parseYAMLTag(field.Tag.Get("yaml"))
			if tag == "-" {
				continue
			}
			if tag == "" {
				tag = strings.ToLower(field.Name)
			}
			fv := v.Field(i)
			if omitempty && fv.IsZero() {
				continue
			}
			// Top-level invocation: sectionPrefix is empty, so this
			// field's tag becomes the section. Nested invocation:
			// append onto the key path.
			if sectionPrefix == "" {
				out = append(out, walkScalarPaths(fv, tag, "")...)
			} else {
				out = append(out, walkScalarPaths(fv, sectionPrefix, joinKey(keyPrefix, tag))...)
			}
		}
		return out
	case reflect.Map:
		if v.Len() == 0 {
			return []string{leafPath(sectionPrefix, keyPrefix)}
		}
		out := []string{}
		keys := v.MapKeys()
		sort.Slice(keys, func(a, b int) bool {
			return keys[a].String() < keys[b].String()
		})
		for _, k := range keys {
			out = append(out, walkScalarPaths(v.MapIndex(k), sectionPrefix, joinKey(keyPrefix, k.String()))...)
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			return []string{leafPath(sectionPrefix, keyPrefix)}
		}
		out := []string{}
		for i := 0; i < v.Len(); i++ {
			idx := joinPath(keyPrefix, "["+itoa(i)+"]")
			out = append(out, walkScalarPaths(v.Index(i), sectionPrefix, idx)...)
		}
		return out
	default:
		return []string{leafPath(sectionPrefix, keyPrefix)}
	}
}

func parseYAMLTag(tag string) (name string, omitempty bool) {
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

func joinKey(prefix, segment string) string {
	return joinPath(prefix, segment)
}

func leafPath(section, key string) string {
	return joinPath(section, key)
}

func itoa(n int) string {
	// Tiny inline strconv replacement avoids dragging fmt into the hot
	// reflect walker — keeps the test file dependency surface lean.
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

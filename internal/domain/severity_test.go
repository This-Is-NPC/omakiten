package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// resetSeverities clears the active registry so tests do not leak state
// across each other. Tests that need a registry call RegisterSeverities
// inside themselves; the cleanup restores the empty state.
func resetSeverities(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		RegisterSeverities(nil)
	})
}

func TestRegisterSeveritiesAndDefault(t *testing.T) {
	cases := map[string]struct {
		pairs   []SeverityPair
		wantDef Severity
	}{
		"explicit default flag wins": {
			pairs: []SeverityPair{
				{ID: 1, Value: "info"},
				{ID: 2, Value: "warning", Default: true},
				{ID: 3, Value: "error"},
			},
			wantDef: Severity(2),
		},
		"no flag — middle entry by index": {
			pairs: []SeverityPair{
				{ID: 1, Value: "info"},
				{ID: 2, Value: "warning"},
				{ID: 3, Value: "error"},
			},
			wantDef: Severity(2),
		},
		"empty registry yields zero default": {
			pairs:   nil,
			wantDef: SeverityZero,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resetSeverities(t)
			RegisterSeverities(tc.pairs)
			if got := DefaultSeverity(); got != tc.wantDef {
				t.Fatalf("DefaultSeverity() = %d, want %d", got, tc.wantDef)
			}
		})
	}
}

func TestSeverityLabelAndString(t *testing.T) {
	resetSeverities(t)
	RegisterSeverities([]SeverityPair{
		{ID: 1, Value: "info"},
		{ID: 2, Value: "warning"},
	})

	if got := Severity(1).Label(); got != "info" {
		t.Fatalf("Severity(1).Label() = %q, want \"info\"", got)
	}
	if got := Severity(99).Label(); got != "" {
		t.Fatalf("unknown id should return empty label, got %q", got)
	}
	if got := Severity(2).String(); got != "warning" {
		t.Fatalf("Severity(2).String() = %q, want \"warning\"", got)
	}
	// Unknown id falls back to numeric so logs are never blank.
	if got := Severity(99).String(); got != "99" {
		t.Fatalf("unknown id .String() should fall back to numeric, got %q", got)
	}
	// Empty registry: Label is empty, String falls back to numeric.
	RegisterSeverities(nil)
	if got := Severity(1).Label(); got != "" {
		t.Fatalf("empty registry .Label() = %q, want \"\"", got)
	}
	if got := Severity(1).String(); got != "1" {
		t.Fatalf("empty registry .String() should be numeric, got %q", got)
	}
}

func TestSeverityMarshalJSON(t *testing.T) {
	resetSeverities(t)
	RegisterSeverities([]SeverityPair{
		{ID: 1, Value: "info"},
		{ID: 2, Value: "warning"},
	})

	got, err := json.Marshal(Severity(1))
	if err != nil {
		t.Fatalf("Marshal err = %v", err)
	}
	if string(got) != `"info"` {
		t.Fatalf("Marshal(known) = %s, want %q", got, `"info"`)
	}

	// Unknown id falls back to numeric so the receiver still gets data.
	got, err = json.Marshal(Severity(99))
	if err != nil {
		t.Fatalf("Marshal err = %v", err)
	}
	if string(got) != "99" {
		t.Fatalf("Marshal(unknown) = %s, want \"99\"", got)
	}

	// Empty registry: every id falls back to numeric.
	RegisterSeverities(nil)
	got, err = json.Marshal(Severity(1))
	if err != nil {
		t.Fatalf("Marshal err = %v", err)
	}
	if string(got) != "1" {
		t.Fatalf("Marshal(empty registry) = %s, want \"1\"", got)
	}
}

func TestSeverityUnmarshalJSON(t *testing.T) {
	resetSeverities(t)
	RegisterSeverities([]SeverityPair{
		{ID: 1, Value: "info"},
		{ID: 2, Value: "warning"},
	})

	cases := map[string]struct {
		data    string
		wantID  Severity
		wantErr string
	}{
		"label resolves to id":     {data: `"info"`, wantID: Severity(1)},
		"numeric id passes through": {data: `2`, wantID: Severity(2)},
		"null becomes zero":        {data: `null`, wantID: SeverityZero},
		"empty string becomes zero": {data: `""`, wantID: SeverityZero},
		"unknown label errors":     {data: `"crit"`, wantErr: "unknown severity label"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var s Severity
			err := json.Unmarshal([]byte(tc.data), &s)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Unmarshal(%s) err = %v, want substring %q", tc.data, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s) err = %v", tc.data, err)
			}
			if s != tc.wantID {
				t.Fatalf("Unmarshal(%s) = %d, want %d", tc.data, s, tc.wantID)
			}
		})
	}
}

// TestSeverityUnmarshalJSONUninitialisedRegistry locks the distinct
// error message thrown when a label arrives before RegisterSeverities
// has been called — surfaces the wiring bug instead of looking like
// a typo.
func TestSeverityUnmarshalJSONUninitialisedRegistry(t *testing.T) {
	resetSeverities(t)
	RegisterSeverities(nil)

	var s Severity
	err := json.Unmarshal([]byte(`"info"`), &s)
	if err == nil {
		t.Fatalf("expected error when registry empty")
	}
	if !strings.Contains(err.Error(), "registry not initialised") {
		t.Fatalf("error = %v, want substring \"registry not initialised\"", err)
	}
}

func TestSeverityFromLabel(t *testing.T) {
	resetSeverities(t)
	RegisterSeverities([]SeverityPair{
		{ID: 1, Value: "info"},
		{ID: 2, Value: "warning"},
	})

	if got, ok := SeverityFromLabel("warning"); !ok || got != Severity(2) {
		t.Fatalf("SeverityFromLabel(\"warning\") = (%d, %v), want (2, true)", got, ok)
	}
	if got, ok := SeverityFromLabel("unknown"); ok || got != SeverityZero {
		t.Fatalf("unknown label should return (0, false), got (%d, %v)", got, ok)
	}
	if got, ok := SeverityFromLabel(""); ok || got != SeverityZero {
		t.Fatalf("empty label should return (0, false), got (%d, %v)", got, ok)
	}

	RegisterSeverities(nil)
	if got, ok := SeverityFromLabel("warning"); ok || got != SeverityZero {
		t.Fatalf("empty registry should return (0, false), got (%d, %v)", got, ok)
	}
}

func TestSeverityIsRegistered(t *testing.T) {
	resetSeverities(t)
	RegisterSeverities([]SeverityPair{
		{ID: 1, Value: "info"},
	})

	if !Severity(1).IsRegistered() {
		t.Fatalf("known id should be registered")
	}
	if Severity(99).IsRegistered() {
		t.Fatalf("unknown id should not be registered")
	}

	RegisterSeverities(nil)
	if Severity(1).IsRegistered() {
		t.Fatalf("empty registry should report not registered")
	}
}

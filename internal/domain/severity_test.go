package domain

import (
	"encoding/json"
	"testing"
)

func TestEnumRegistrySeverityDefault(t *testing.T) {
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
			r := NewEnumRegistry(nil, tc.pairs)
			if got := r.DefaultSeverity(); got != tc.wantDef {
				t.Fatalf("DefaultSeverity() = %d, want %d", got, tc.wantDef)
			}
		})
	}
}

func TestEnumRegistrySeverityLabel(t *testing.T) {
	r := NewEnumRegistry(nil, []SeverityPair{
		{ID: 1, Value: "info"},
		{ID: 2, Value: "warning"},
	})

	if got := r.SeverityLabel(Severity(1)); got != "info" {
		t.Fatalf("SeverityLabel(1) = %q, want \"info\"", got)
	}
	if got := r.SeverityLabel(Severity(99)); got != "" {
		t.Fatalf("unknown id should return empty label, got %q", got)
	}

	empty := NewEnumRegistry(nil, nil)
	if got := empty.SeverityLabel(Severity(1)); got != "" {
		t.Fatalf("empty registry SeverityLabel = %q, want \"\"", got)
	}
}

// TestSeverityMarshalsAsInt locks the wire format: Severity always
// serializes as its raw int id. Boundary layers project to label strings
// via DTOs that hold a registry.
func TestSeverityMarshalsAsInt(t *testing.T) {
	data, err := json.Marshal(Severity(2))
	if err != nil {
		t.Fatalf("Marshal err = %v", err)
	}
	if string(data) != "2" {
		t.Fatalf("Marshal(2) = %s, want 2", data)
	}

	var got Severity
	if err := json.Unmarshal([]byte(`5`), &got); err != nil {
		t.Fatalf("Unmarshal(5) err = %v", err)
	}
	if got != Severity(5) {
		t.Fatalf("Unmarshal(5) = %d, want 5", got)
	}
}

func TestEnumRegistrySeverityFromLabel(t *testing.T) {
	r := NewEnumRegistry(nil, []SeverityPair{
		{ID: 1, Value: "info"},
		{ID: 2, Value: "warning"},
	})

	if got, ok := r.SeverityFromLabel("warning"); !ok || got != Severity(2) {
		t.Fatalf("SeverityFromLabel(\"warning\") = (%d, %v), want (2, true)", got, ok)
	}
	if got, ok := r.SeverityFromLabel("unknown"); ok || got != SeverityZero {
		t.Fatalf("unknown label should return (0, false), got (%d, %v)", got, ok)
	}
	if got, ok := r.SeverityFromLabel(""); ok || got != SeverityZero {
		t.Fatalf("empty label should return (0, false), got (%d, %v)", got, ok)
	}

	empty := NewEnumRegistry(nil, nil)
	if got, ok := empty.SeverityFromLabel("warning"); ok || got != SeverityZero {
		t.Fatalf("empty registry should return (0, false), got (%d, %v)", got, ok)
	}
}

func TestEnumRegistryIsSeverityRegistered(t *testing.T) {
	r := NewEnumRegistry(nil, []SeverityPair{
		{ID: 1, Value: "info"},
	})

	if !r.IsSeverityRegistered(Severity(1)) {
		t.Fatalf("known id should be registered")
	}
	if r.IsSeverityRegistered(Severity(99)) {
		t.Fatalf("unknown id should not be registered")
	}

	empty := NewEnumRegistry(nil, nil)
	if empty.IsSeverityRegistered(Severity(1)) {
		t.Fatalf("empty registry should report not registered")
	}
}

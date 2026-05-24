package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/domain"
)

// TestSaveBundleRejectsOversizedWiringHeader pins the W5 #246 fix:
// readHeaderComments must route through readFileBounded(MaxWiringFile-
// Bytes) instead of the unbounded os.ReadFile, and SaveBundle must
// surface ErrConfigTooLarge when the on-disk wiring already exceeds
// the cap. Without the cap the save path would mmap a multi-GB file
// to preserve its header — symmetric to the load-side cap W5 #220
// added on entity_loader / language_loader / loader.
func TestSaveBundleRejectsOversizedWiringHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "omakiten.yaml")

	// Seed an on-disk wiring that overruns MaxWiringFileBytes by one
	// byte. The body itself is YAML-shaped only superficially; the
	// guard fires on size, not parse validity, so the content of the
	// overflow region does not matter.
	oversize := make([]byte, MaxWiringFileBytes+1)
	for i := range oversize {
		oversize[i] = '#'
	}
	if err := os.WriteFile(path, oversize, 0o600); err != nil {
		t.Fatalf("seed oversize wiring: %v", err)
	}

	bundle := Bundle{
		Version: 1,
		Workflows: []Workflow{{
			ID:      1,
			Key:     "demo",
			Name:    "Demo",
			Buckets: []Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
		}},
		Config: Settings{Workflow: WorkflowSettings{Active: "demo"}},
	}
	err := SaveBundle(path, bundle)
	if err == nil {
		t.Fatalf("SaveBundle accepted oversized wiring; expected ErrConfigTooLarge")
	}
	if !IsConfigTooLarge(err) {
		t.Fatalf("SaveBundle returned %v, want ErrConfigTooLarge", err)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error is not coded: %T", err)
	}
	if coded.Code != domain.ErrConfigTooLarge {
		t.Fatalf("code = %q, want %q", coded.Code, domain.ErrConfigTooLarge)
	}
	// Path detail must name the file the rule fired on so an operator
	// can grep + truncate without guessing.
	if coded.Details["path"] != path {
		t.Fatalf("details.path = %v, want %q", coded.Details["path"], path)
	}
}

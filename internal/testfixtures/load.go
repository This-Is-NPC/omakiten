// Package testfixtures wires test packages to the YAML config files that
// live next to them under testdata/. The convention is one fixture per
// scenario (e.g. policy_comment_inherits_task.yaml), each documenting at
// the top of the file what shape it covers, so a future reader can map
// "what is this YAML for?" without re-reading the test that loads it.
//
// Why a shared package: every layer of the codebase (config/app/sqlite/
// tui/agent/mcp) needs to construct config.Bundle values for tests.
// Inline Go construction drifts from the real parser path; loading from
// real YAML keeps test inputs identical to what production sees from
// `defaults/omakiten.yaml`. The helper is intentionally minimal — no
// magic — so callers can extend it locally if a scenario needs more.
package testfixtures

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// RegisterCanonicalPriorities installs the kit's default 1=low /
// 2=normal / 3=high priority registry into the domain package. Tests
// that need to round-trip Priority through JSON marshaling, validate
// PriorityFromLabel, or assert on Priority.Label() should call this in
// setup. The runtime composition roots (cli, agentruntime) install the
// configured table from the loaded bundle automatically; tests that
// bypass those roots have to seed the registry themselves so their
// scenarios reflect production semantics.
func RegisterCanonicalPriorities() {
	defs := config.CanonicalPriorities
	pairs := make([]domain.PriorityPair, len(defs))
	for i, d := range defs {
		pairs[i] = domain.PriorityPair{ID: d.ID, Value: d.Value}
	}
	domain.RegisterPriorities(pairs)
}

// LoadBundle reads <package-dir>/testdata/<name> and unmarshals it as a
// config.Bundle. The relative path is resolved against the test binary's
// working directory, which Go sets to the package directory for test
// runs — meaning a fixture at internal/app/testdata/foo.yaml is reached
// as LoadBundle(t, "foo.yaml") from any test in internal/app.
//
// Failures terminate the test via t.Fatalf so call sites do not have to
// thread error returns through helper chains.
func LoadBundle(t testing.TB, name string) config.Bundle {
	t.Helper()
	if filepath.IsAbs(name) {
		return loadFromPath(t, name)
	}
	return loadFromPath(t, filepath.Join("testdata", name))
}

// LoadBundleFromAbsPath is the explicit-path variant for the rare test
// that wants to point at a fixture outside its own testdata/ dir (e.g.
// integration tests that load `defaults/omakiten.yaml` to assert the
// shipped kit parses cleanly). Most callers should prefer LoadBundle.
func LoadBundleFromAbsPath(t testing.TB, path string) config.Bundle {
	t.Helper()
	return loadFromPath(t, path)
}

func loadFromPath(t testing.TB, path string) config.Bundle {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("testfixtures: read %q: %v", path, err)
	}
	// KnownFields(true) makes typos and dead blocks fail loudly. Bundle
	// marks Skills/Personas/Laws/Templates/Projects/MCPCommands as
	// `yaml:"-"` because production loads them from per-entity folders;
	// silently dropping those keys from a fixture would make scenarios
	// look richer than they actually are. Tests that need those entities
	// must wire them inline in Go after LoadBundle returns.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var bundle config.Bundle
	if err := dec.Decode(&bundle); err != nil {
		t.Fatalf("testfixtures: parse %q: %v", path, err)
	}
	return bundle
}

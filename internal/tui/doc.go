// Package tui is the bubbletea-based terminal UI. The root Model owns global
// lifecycle, navigation, overlays, services, refresh guards, and the legacy
// state for screens not yet extracted. Addressable routes are declared once in
// screen_registry.go against the dependency-neutral screenhost contract.
//
// Extracted screens (including the Board, Table, Graph and Plans task lenses and
// the plan-goal reader) live in their own packages under internal/tui/screens/
// and implement screenhost.Screen;
// screen_host.go is the root half of that contract
// (bind host deps, resolve the live instance for the descriptor factory,
// dispatch keys, fold the semantic outcome back into root state). Reusable
// stateful components live under internal/tui/components/, and the shared
// rendering algorithms both root and screens draw with live in
// internal/tui/components/screenkit.
package tui

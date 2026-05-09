// Package sqlite is the leaf adapter that persists Omakiten state in a
// local SQLite database. Implements the ports declared in
// internal/app/ports.go (Tasks, Comments, Events, Tags, Workflow,
// Errors, …) plus the migrations runner. SQL is parameterised
// throughout — query strings never embed user input — and every list
// endpoint indexes through a named idx_* (see migrations/) so plans
// stay index-driven.
package sqlite

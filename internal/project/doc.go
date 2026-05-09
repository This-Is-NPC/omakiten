// Package project resolves the active project for a given runtime
// invocation. Resolution order: explicit --project flag, then the
// closest ancestor of cwd that matches a registered project root,
// falling back to a coded "no active project" error so the boundary
// layers can prompt the user instead of writing to the wrong project.
package project

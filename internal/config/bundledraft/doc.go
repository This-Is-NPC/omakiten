// Package bundledraft is the staged-edit engine over a canonical
// [config.Bundle]: it clones a loaded bundle, applies typed workflow, guard,
// permission and command mutations to the candidate, validates and diffs the
// pair, previews the task impact of a bucket removal or rekey, and commits the
// candidate through a bundle-editor port with baseline-drift detection and
// current-path atomic publication.
//
// It lives beside the schema and the validator it enforces rather than inside
// the Studio screen that used to own it: the screen declares the port, turns
// the report into props and paints. Nothing here knows about terminals.
package bundledraft

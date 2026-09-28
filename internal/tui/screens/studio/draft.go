package studio

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
)

// The staged bundle-mutation engine Studio drives lives in
// omakiten/internal/config/bundledraft, beside the schema and the validator it
// enforces. What stays here is the screen's vocabulary for it: the port and the
// names the Studio bodies and the TUI host already spell, bound to the engine's
// types so a screen keeps translating a report into props, never owning one.

// StudioDraft is the staged bundle-mutation port Studio consumes. It is the
// whole surface a Studio session may drive against the bundle it opened at
// entry: read the candidate, mutate it, weigh the impact of a bucket change,
// and commit it. *bundledraft.Draft satisfies it; the screen names the port and
// the host wires the engine, so no Studio file has to hold the mutation,
// validation logic to paint a report of it.
type StudioDraft interface {
	AssuranceSnapshot() (string, error)
	BindText(text bundledraft.Text)
	Report() bundledraft.Report
	ReportText(text bundledraft.Text) bundledraft.Report
	Candidate() config.Bundle
	Dirty() bool
	Mutate(mutate func(*config.Bundle) error) bundledraft.Report
	SetCommandSpec(key string, spec config.CommandSpec) bundledraft.Report
	RenameBucket(bucketID int, name string) bundledraft.Report
	ChangeBucketKey(bucketID int, key string) bundledraft.Report
	AddBucket(key, name string) bundledraft.Report
	MoveBucket(bucketID int, delta int) bundledraft.Report
	SetBucketPermission(bucketID int, entity bundledraft.BucketPermissionEntity, op bundledraft.BucketPermissionOp, allowed bool) bundledraft.Report
	AddTransition(fromBucketID, toBucketID int) bundledraft.Report
	RemoveTransition(fromBucketID, toBucketID int) bundledraft.Report
	AddGuard(kind bundledraft.GuardSetKind, fromBucketID, toBucketID int, guard config.TransitionGuard) bundledraft.Report
	SetGuard(kind bundledraft.GuardSetKind, fromBucketID, toBucketID int, index int, guard config.TransitionGuard) bundledraft.Report
	RemoveGuard(kind bundledraft.GuardSetKind, fromBucketID, toBucketID int, index int) bundledraft.Report
	MoveGuard(kind bundledraft.GuardSetKind, fromBucketID, toBucketID int, index, delta int) bundledraft.Report
	DeleteBucket(ctx context.Context, counter bundledraft.BucketTaskCounter, bucketID int) bundledraft.Report
	ImpactPreview(ctx context.Context, counter bundledraft.BucketTaskCounter) bundledraft.ImpactPreview
	Apply(ctx context.Context, reload func(string) error) (config.Bundle, error)
}

const (
	StudioBucketPermissionTask    = bundledraft.BucketPermissionTask
	StudioBucketPermissionComment = bundledraft.BucketPermissionComment
	StudioBucketPermissionCreate  = bundledraft.BucketPermissionCreate
	StudioBucketPermissionEdit    = bundledraft.BucketPermissionEdit
	StudioBucketPermissionDelete  = bundledraft.BucketPermissionDelete
	StudioGuardSetTransition      = bundledraft.GuardSetTransition
	StudioGuardSetArchive         = bundledraft.GuardSetArchive
	StudioGuardSetDelete          = bundledraft.GuardSetDelete
	StudioGuardSetUnarchive       = bundledraft.GuardSetUnarchive
)

// DiffStudioBundles summarises a candidate against its baseline.
var DiffStudioBundles = bundledraft.Diff

// CloneBundle deep copies a bundle so two Studio surfaces never share backing
// arrays.
func CloneBundle(in config.Bundle) config.Bundle { return bundledraft.CloneBundle(in) }

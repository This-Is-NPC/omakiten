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
	BindText(text Text)
	Report() StudioDraftReport
	ReportText(text Text) StudioDraftReport
	Candidate() config.Bundle
	Dirty() bool
	Mutate(mutate func(*config.Bundle) error) StudioDraftReport
	SetMCPCommandSpec(key string, spec config.MCPCommandSpec) StudioDraftReport
	RenameBucket(bucketID int, name string) StudioDraftReport
	ChangeBucketKey(bucketID int, key string) StudioDraftReport
	AddBucket(key, name string) StudioDraftReport
	MoveBucket(bucketID int, delta int) StudioDraftReport
	SetBucketPermission(bucketID int, entity StudioBucketPermissionEntity, op StudioBucketPermissionOp, allowed bool) StudioDraftReport
	AddTransition(fromBucketID, toBucketID int) StudioDraftReport
	RemoveTransition(fromBucketID, toBucketID int) StudioDraftReport
	AddGuard(kind StudioGuardSetKind, fromBucketID, toBucketID int, guard config.TransitionGuard) StudioDraftReport
	SetGuard(kind StudioGuardSetKind, fromBucketID, toBucketID int, index int, guard config.TransitionGuard) StudioDraftReport
	RemoveGuard(kind StudioGuardSetKind, fromBucketID, toBucketID int, index int) StudioDraftReport
	MoveGuard(kind StudioGuardSetKind, fromBucketID, toBucketID int, index, delta int) StudioDraftReport
	DeleteBucket(ctx context.Context, counter StudioBucketTaskCounter, bucketID int) StudioDraftReport
	ImpactPreview(ctx context.Context, counter StudioBucketTaskCounter) StudioImpactPreview
	Apply(ctx context.Context, reload func(string) error) (config.Bundle, error)
}

type (
	StudioDraftReport            = bundledraft.Report
	StudioImpactPreview          = bundledraft.ImpactPreview
	StudioBucketImpact           = bundledraft.BucketImpact
	StudioBucketKeyChange        = bundledraft.BucketKeyChange
	StudioFinalBucketChange      = bundledraft.FinalBucketChange
	StudioBucketPermissionEntity = bundledraft.BucketPermissionEntity
	StudioBucketPermissionOp     = bundledraft.BucketPermissionOp
	StudioGuardSetKind           = bundledraft.GuardSetKind
	StudioBucketTaskCounter      = bundledraft.BucketTaskCounter
)

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

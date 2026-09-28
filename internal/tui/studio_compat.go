package tui

import (
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/tui/screens/studio"
)

// Draft aliases keep the non-UI Studio API stable while ownership lives with
// the extracted screen package.
type StudioDraft = studio.StudioDraft
type StudioDraftReport = studio.StudioDraftReport
type StudioImpactPreview = studio.StudioImpactPreview
type StudioBucketImpact = studio.StudioBucketImpact
type StudioBucketKeyChange = studio.StudioBucketKeyChange
type StudioFinalBucketChange = studio.StudioFinalBucketChange
type StudioBucketPermissionEntity = studio.StudioBucketPermissionEntity
type StudioBucketPermissionOp = studio.StudioBucketPermissionOp
type StudioGuardSetKind = studio.StudioGuardSetKind
type StudioBucketTaskCounter = studio.StudioBucketTaskCounter

const (
	StudioBucketPermissionTask    = studio.StudioBucketPermissionTask
	StudioBucketPermissionComment = studio.StudioBucketPermissionComment
	StudioBucketPermissionCreate  = studio.StudioBucketPermissionCreate
	StudioBucketPermissionEdit    = studio.StudioBucketPermissionEdit
	StudioBucketPermissionDelete  = studio.StudioBucketPermissionDelete
	StudioGuardSetTransition      = studio.StudioGuardSetTransition
	StudioGuardSetArchive         = studio.StudioGuardSetArchive
	StudioGuardSetDelete          = studio.StudioGuardSetDelete
	StudioGuardSetUnarchive       = studio.StudioGuardSetUnarchive
)

// NewStudioDraft opens a draft the way the Studio host does — the engine
// behind the screen's port, over the editor the runtime wired.
var NewStudioDraft = bundledraft.New
var applyBundleEditor = bundledraft.ApplyPlanned
var DiffStudioBundles = studio.DiffStudioBundles
var StudioFlowWarnings = studio.StudioFlowWarnings
var cloneBundle = studio.CloneBundle

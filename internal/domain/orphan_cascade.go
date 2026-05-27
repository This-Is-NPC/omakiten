package domain

// OrphanCascadePlan bundles every resolver pair + kit identity the
// sub-task kit cascade migration (#281 / #285) needs to preview or
// rebind orphaned tasks. Root-tree rows resolve against
// (CurrentRoot, PreviousRoot); sub-task rows resolve against
// (CurrentSub, PreviousSub). FromKit / ToKit name the kit identities
// the task.bucket_orphaned payload locks (#281 review §11).
//
// Introduced per locked decision on task #301: the same plan feeds
// both `OrphanRepository.PreviewOrphanedCascade` and
// `OrphanRepository.RebindOrphanedCascade` so the preview shown in
// the bundle-swap prompt matches the rows the confirmed migrate
// rewrites byte-for-byte. The struct sits in the domain package so
// the sqlite adapter consumes it without an upward import.
type OrphanCascadePlan struct {
	CurrentRoot  BucketResolver
	PreviousRoot BucketResolver
	CurrentSub   BucketResolver
	PreviousSub  BucketResolver
	FromKit      string
	ToKit        string
}

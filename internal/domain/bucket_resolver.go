package domain

// BucketResolver translates bucket keys/ids through whatever
// per-project view the caller holds. *config.Snapshot satisfies
// this implicitly (its BucketByKey/BucketByID/Workflow methods
// already return domain types). Sqlite methods that need
// key↔id resolution accept this interface so the adapter never
// imports the config package.
//
// Workflow returns the resolved workflow shape (buckets +
// transitions) the resolver corresponds to. The orphan flow uses
// it to enumerate the active bucket set without dragging config
// types into the adapter.
//
// KitKey returns the identity (`kit.key`) of the kit that produced
// this resolver. The sqlite events helper threads it into the
// `resolved_kit` field on task event payloads without having to
// reach back into the config package — review finding §C.13 of #297.
type BucketResolver interface {
	BucketByKey(key string) (Bucket, bool)
	BucketByID(id int64) (Bucket, bool)
	Workflow() Workflow
	KitKey() string
}

package sqlite

import (
	"context"

	"omakiten/internal/domain"
)

// activeBucketID resolves a bucket key to its id via the caller's
// BucketResolver. Used by the connection-pool and in-tx code paths
// alike — the resolver read does not touch the SQL connection so it is
// safe from either context.
func (s *Store) activeBucketID(_ context.Context, key string, buckets domain.BucketResolver) (int64, error) {
	if isNilResolver(buckets) {
		return 0, domain.NewError(domain.ErrBucketNotFound, "bucket resolver is required", map[string]any{"bucket": key})
	}
	b, ok := buckets.BucketByKey(key)
	if !ok {
		return 0, domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": key})
	}
	return b.ID, nil
}

// bucketKeyByID resolves a bucket id to its key via the caller's
// BucketResolver. Returns "" when the id is 0, the resolver is nil, or
// the bucket is missing from the resolver (matches the pre-migration
// "no rows -> empty key" behaviour the move-event recorder depends on).
func (s *Store) bucketKeyByID(bucketID int64, buckets domain.BucketResolver) string {
	if bucketID == 0 || isNilResolver(buckets) {
		return ""
	}
	bucket, ok := buckets.BucketByID(bucketID)
	if !ok {
		return ""
	}
	return bucket.Key
}

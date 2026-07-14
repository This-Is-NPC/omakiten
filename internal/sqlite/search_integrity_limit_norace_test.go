//go:build !race

package sqlite

import "time"

const (
	searchIndexPerformanceLimit     = 5 * time.Second
	searchIndexReindexLockLimit     = 5 * time.Second
	searchIndexLargeCorruptionLimit = 5 * time.Second
)

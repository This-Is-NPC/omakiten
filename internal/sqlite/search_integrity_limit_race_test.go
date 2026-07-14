//go:build race

package sqlite

import "time"

const (
	searchIndexPerformanceLimit     = 15 * time.Second
	searchIndexReindexLockLimit     = 15 * time.Second
	searchIndexLargeCorruptionLimit = 45 * time.Second
)

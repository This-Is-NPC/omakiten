package agentruntime

import (
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
)

// Session contains the resources a composition root supplies to an interactive adapter.
type Session struct {
	CacheProjectID int64
	Store          *sqlite.Store
	Cache          *BundleCache
	Project        domain.ProjectContext
	ConfigPath     string
	DBPath         string
	RepoLocalDir   string
	Version        string
	Snapshot       *config.Snapshot
}

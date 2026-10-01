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

// Maintenance is database maintenance on the session's database for
// surface, keeping the session's backup retention; it can delete projects.
func (s Session) Maintenance(surface config.Surface) *Maintenance {
	return NewMaintenance(MaintenanceOptions{
		DBPath:    s.DBPath,
		Retention: s.Snapshot.Settings().Backup.RetentionCount,
		Projects:  s.Store,
		Catalog:   s.Snapshot.Catalog(surface),
	})
}

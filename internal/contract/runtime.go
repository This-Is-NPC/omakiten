package contract

import (
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// RuntimeView supplies immutable bundle data and operation ports to a delivery adapter.
type RuntimeView struct {
	Service          Operations
	Snapshot         *config.Snapshot
	PreviousSnapshot *config.Snapshot
	EnumRegistry     *domain.EnumRegistry
	Editor           BundleEditor
	SourcePath       string
}

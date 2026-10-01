package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// MaintenanceOperations repairs project state after configuration changes.
type MaintenanceOperations interface {
	MigrateOrphans(ctx context.Context, input contract.MigrateOrphansInput) (contract.MigrateOrphansResponse, error)
}

// MigrateOrphansBody is the migrateOrphans request body.
type MigrateOrphansBody struct {
	// Confirmed applies the rebind; without it the call only previews.
	Confirmed bool `json:"confirmed,omitempty"`
}

func (s *Server) maintenanceRoutes() []route {
	return []route{
		command("migrateOrphans", http.MethodPost, projectPath+"/workflow/orphans", "orphans.migrate", "Preview or apply the rebind of tasks left on removed buckets.", []param{projectParam}, s.migrateOrphans),
	}
}

func (s *Server) migrateOrphans(r *http.Request, body MigrateOrphansBody) (contract.MigrateOrphansResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.MigrateOrphansResponse{}, err
	}
	return ops.MigrateOrphans(r.Context(), contract.MigrateOrphansInput{ProjectSelector: selector, Confirmed: body.Confirmed})
}

package agent

import "omakiten/internal/domain"

type MigrateOrphansInput struct {
	ProjectSelector
	Confirmed bool `json:"confirmed,omitempty"`
}

type MigrateOrphansResponse struct {
	Project      ProjectSummary       `json:"project"`
	Report       domain.OrphanReport  `json:"report"`
	Applied      bool                 `json:"applied"`
	Confirmation Confirmation         `json:"confirmation,omitempty"`
}

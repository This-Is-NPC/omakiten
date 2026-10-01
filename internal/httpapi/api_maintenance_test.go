package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"migrateOrphans", http.MethodPost, "/api/v1/projects/alpha/workflow/orphans", `{"confirmed":true}`, "MigrateOrphans", contract.MigrateOrphansInput{ProjectSelector: alpha, Confirmed: true}},
})

func (f *fakeOps) MigrateOrphans(_ context.Context, in contract.MigrateOrphansInput) (contract.MigrateOrphansResponse, error) {
	f.record("MigrateOrphans", in)
	return contract.MigrateOrphansResponse{}, nil
}

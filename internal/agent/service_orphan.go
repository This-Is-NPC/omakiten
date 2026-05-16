package agent

import (
	"context"
	"fmt"
)

// MigrateOrphans previews or applies the orphan-task rebind for the active
// project. Without confirmed=true it returns a preview report plus a
// Confirmation block listing affected tasks (mirrors the tasks.delete two-
// phase pattern). With confirmed=true it applies the rebind and returns the
// final report.
//
// When the preview report is empty the operation is a no-op regardless of
// the confirmed flag — there is nothing to mutate and nothing to confirm.
//
// The orphan service is injected by the runtime composition root via
// SetOrphanService; it carries the current + previous Snapshot pair the
// rebind needs to resolve task.bucket_id → previous key across a swap.
// Tests that do not wire one will trip the explicit error rather than
// silently exercising a half-built service.
func (s *Service) MigrateOrphans(ctx context.Context, input MigrateOrphansInput) (MigrateOrphansResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return MigrateOrphansResponse{}, err
	}

	if s.orphanSvc == nil {
		return MigrateOrphansResponse{}, fmt.Errorf("agent: orphan service not installed (call Service.SetOrphanService during runtime composition)")
	}

	preview, err := s.orphanSvc.Preview(ctx, project)
	if err != nil {
		return MigrateOrphansResponse{}, err
	}

	if preview.Total == 0 {
		return MigrateOrphansResponse{
			Project: projectSummary(project),
			Report:  preview,
			Applied: false,
		}, nil
	}

	if !input.Confirmed {
		return MigrateOrphansResponse{
			Project: projectSummary(project),
			Report:  preview,
			Applied: false,
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason: fmt.Sprintf(
					"Workflow changed: %d task(s) will be rebinded to a new bucket. Retry with confirmed=true to apply, or skip to leave them attached to the deactivated bucket.",
					preview.Total),
				Options: []ConfirmationOption{
					{Action: "confirm_migrate", Label: "Retry orphans.migrate with confirmed=true to apply the rebind"},
					{Action: "skip", Label: "Do nothing — tasks remain on the inactive bucket until re-imported"},
				},
			},
		}, nil
	}

	applied, err := s.orphanSvc.Migrate(ctx, project)
	if err != nil {
		return MigrateOrphansResponse{}, err
	}
	return MigrateOrphansResponse{
		Project: projectSummary(project),
		Report:  applied,
		Applied: true,
	}, nil
}

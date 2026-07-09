package agent

import (
	"context"
	"errors"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// InsightsSummary returns the six today-insights scoped to the resolved
// project. Scope resolution is contextual-first:
//
//  1. An explicit input.ProjectID (> 0) wins — the caller asked for that
//     project's reading, wherever it is running from. The response's
//     `project` field then echoes the PINNED project's identity (resolved
//     by id), or is omitted when the pinned id resolves to nothing — the
//     frozen contract must never label project B's data with project A's
//     identity.
//  2. Otherwise the project resolved from the selector (cwd / slug / id)
//     scopes the reading — an agent self-consulting from inside a project
//     root sees ONLY that project's insights, mirroring the TUI screen.
//  3. Only when no ProjectID is supplied AND the selector resolves to
//     nothing (project_not_found) does the call fall through to the
//     cross-project global view (projectID=0, `project` omitted).
//
// Resolver failures other than project_not_found (ambiguous selector, db
// errors) PROPAGATE — failing open into the global view on an arbitrary
// error would silently widen the data scope.
//
// The stuck-task scan targets the in-flight bucket ids of this service's
// active workflow snapshot. An explicit cross-project pin therefore scans
// with the caller's workflow roster — acceptable for the local single-user
// server; the per-project service resolver hands cross-project calls their
// own service in production.
//
// This surface is read-only and consultivo: it ONLY computes and returns
// insight data via app.InsightsService.Today, which is itself a read-only
// query orchestration. It never moves a task, relaxes a guard, gates a
// transition, or mutates any state. The response carries an explicit
// schema_version so consuming agents can pin the frozen contract.
func (s *Service) InsightsSummary(ctx context.Context, input InsightsSummaryInput) (InsightsSummaryResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		if !isProjectNotFound(err) {
			return InsightsSummaryResponse{}, err
		}
		// Not-found only: global-view fallthrough — see godoc above.
		project = domain.ProjectContext{}
	}
	projectID := input.ProjectID
	if projectID <= 0 {
		projectID = project.ID
	} else if projectID != project.ID {
		// Explicit pin diverges from the resolved context: re-resolve the
		// pinned id so the echoed identity matches the data. A not-found pin
		// omits the identity (the data still comes back, unlabeled); any
		// other resolver error propagates, mirroring the primary path — a db
		// failure must not silently degrade to an omitted identity.
		pinned, pinErr := s.resolveProject(ctx, ProjectSelector{ProjectID: projectID})
		switch {
		case pinErr == nil:
			project = pinned
		case isProjectNotFound(pinErr):
			project = domain.ProjectContext{}
		default:
			return InsightsSummaryResponse{}, pinErr
		}
	}

	// stuckBuckets is tri-state (see sqlite.Store.Insights): nil = no
	// workflow resolved (canonical fallback); non-nil = authoritative, empty
	// when the preset has no in-flight stage.
	var stuckBuckets []int64
	if s.snapshot != nil {
		ids, ok := s.snapshot.Workflow().InFlightBucketIDs()
		if ok {
			if ids == nil {
				ids = []int64{}
			}
			stuckBuckets = ids
		}
	}

	insights, err := app.NewInsightsService(s.repo).Today(ctx, project, projectID, input.StuckDays, stuckBuckets)
	if err != nil {
		return InsightsSummaryResponse{}, err
	}
	var summary *ProjectSummary
	if project.ID > 0 {
		p := projectSummary(project)
		summary = &p
	}
	return InsightsSummaryResponse{
		SchemaVersion: InsightsSummarySchemaVersion,
		Project:       summary,
		Insights:      toInsightsSummaryBoard(insights),
	}, nil
}

// isProjectNotFound reports whether err is the resolver's
// project_not_found coded error — the one resolution failure the insights
// surface treats as "fall through to global / omit identity" rather than
// propagating. Every other error (ambiguous selector, db failure) is a real
// fault the caller must see.
func isProjectNotFound(err error) bool {
	var coded *domain.CodedError
	return errors.As(err, &coded) && coded.Code == domain.ErrProjectNotFound
}

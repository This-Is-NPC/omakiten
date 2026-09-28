package operation

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func (s *Service) BoardSnapshot(ctx context.Context, input contract.BoardSnapshotInput) (contract.BoardSnapshot, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.BoardSnapshot{}, err
	}
	query := app.NewTUIQueryService(s.repo, s.snapshot, s.repo, s.repo, s.repo)
	snap, err := query.Snapshot(ctx, project, input.Sort, app.SnapshotOptions{IncludeArchived: input.IncludeArchived})
	if err != nil {
		return contract.BoardSnapshot{}, err
	}
	return contract.BoardSnapshot{
		Tasks:        snap.Tasks,
		Workflow:     snap.Workflow,
		Dependencies: snap.Dependencies,
		Comments:     snap.Comments,
		Laws:         snap.Laws,
		Skills:       snap.Skills,
		Personas:     snap.Personas,
		Templates:    snap.Templates,
		AllTags:      snap.AllTags,
		TaskTagsByID: snap.TaskTagsByID,
	}, nil
}

func (s *Service) ListPlanRollups(ctx context.Context, input contract.ProjectSelector) ([]domain.PlanRollup, error) {
	project, err := s.resolveProject(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.newPlanService().ListRollups(ctx, project)
}

func (s *Service) ShowPlanView(ctx context.Context, input contract.ShowPlanInput) (domain.PlanShow, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return domain.PlanShow{}, err
	}
	return s.newPlanService().Show(ctx, project, input.Slug)
}

func (s *Service) SyncBlockers(ctx context.Context, input contract.SyncBlockersInput) error {
	// The TUI picker is the census path for both unit ops (add and
	// remove in one save). Gate both slugs so a tui:false row cannot
	// write through this extra.
	if err := s.allow("dependency.add"); err != nil {
		return err
	}
	if err := s.allow("dependency.remove"); err != nil {
		return err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return err
	}
	return app.NewDependencyService(s.repo).SyncBlockers(ctx, project, input.TaskID, input.TaskIDs)
}

func (s *Service) QueryComments(ctx context.Context, project domain.ProjectContext, filter domain.CommentFilter) ([]domain.Comment, error) {
	return s.newCommentService().Query(ctx, project, filter)
}

func (s *Service) InsightsToday(ctx context.Context, project domain.ProjectContext, projectID int64, stuckDays int, stuckBuckets []int64) (domain.Insights, error) {
	return app.NewInsightsService(s.repo).Today(ctx, project, projectID, stuckDays, stuckBuckets)
}

func (s *Service) ResolveBucketPermissions(ctx context.Context, project domain.ProjectContext, taskID int64, entity, op string) (bool, string, error) {
	if s.workflow == nil {
		return true, "", nil
	}
	return s.workflow.ResolveBucketPermissions(ctx, project, taskID, entity, op)
}

func (s *Service) EmitGuardViolated(ctx context.Context, projectID int64, entityType string, entityID int64, operation, rule, hint string, target map[string]any) {
	if s.workflow == nil {
		return
	}
	s.workflow.Evaluator().EmitViolated(ctx, projectID, entityType, entityID, operation, rule, hint, target)
}

func (s *Service) SetTemplateDefault(ctx context.Context, slug, kind, projectSlug string) error {
	if s.entity.editor == nil || s.entity.files == nil {
		return domain.NewError(domain.ErrValidation, "template authoring is not wired", nil)
	}
	return app.NewTemplateService(s.entity.editor, s.entity.files).SetDefault(ctx, slug, kind, projectSlug)
}

// NormalizeTagName re-exports the app helper so TUI/CLI composition does
// not import internal/app (D16/D18).
func NormalizeTagName(raw string, synonyms map[string]string) string {
	return app.NormalizeTagName(raw, synonyms)
}

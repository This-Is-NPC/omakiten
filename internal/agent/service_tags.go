package agent

import (
	"context"

	"omakiten/internal/app"
)

func (s *Service) AddTag(ctx context.Context, input AddTagInput) (TagResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return TagResponse{}, err
	}
	tag, err := app.NewTagServiceWithEvents(s.repo, s.repo).Add(ctx, project, input.EntityType, input.EntityID, input.TagName)
	if err != nil {
		return TagResponse{}, err
	}
	return TagResponse{Project: projectSummary(project), Tag: tagSummary(tag)}, nil
}

func (s *Service) RemoveTag(ctx context.Context, input RemoveTagInput) (RemoveTagResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RemoveTagResponse{}, err
	}
	if !input.Confirmed {
		return RemoveTagResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a tag is irreversible and requires explicit confirmation.",
				Options:              []ConfirmationOption{{Action: "remove_tag", Label: "Retry with confirmed=true to remove it"}},
			},
		}, nil
	}
	if err := app.NewTagServiceWithEvents(s.repo, s.repo).Remove(ctx, project, input.EntityType, input.EntityID, input.TagID); err != nil {
		return RemoveTagResponse{}, err
	}
	return RemoveTagResponse{Project: projectSummary(project), Removed: true}, nil
}

func (s *Service) ListTags(ctx context.Context, input ListTagsInput) (TagListResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return TagListResponse{}, err
	}
	tags, err := app.NewTagServiceWithEvents(s.repo, s.repo).List(ctx, project, input.EntityType, input.EntityID)
	if err != nil {
		return TagListResponse{}, err
	}
	return TagListResponse{Project: projectSummary(project), Tags: tagSummaries(tags)}, nil
}

func (s *Service) ListAllTags(ctx context.Context) (AllTagsResponse, error) {
	tags, err := app.NewTagServiceWithEvents(s.repo, s.repo).ListAll(ctx)
	if err != nil {
		return AllTagsResponse{}, err
	}
	return AllTagsResponse{Tags: tagSummaries(tags)}, nil
}

func (s *Service) MergeTags(ctx context.Context, input MergeTagsInput) (TagResponse, error) {
	tag, err := app.NewTagServiceWithEvents(s.repo, s.repo).Merge(ctx, input.SourceTagID, input.TargetTagID)
	if err != nil {
		return TagResponse{}, err
	}
	return TagResponse{Tag: tagSummary(tag)}, nil
}

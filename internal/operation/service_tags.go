package operation

import (
	"context"

	"omakiten/internal/contract"
)

func (s *Service) AddTag(ctx context.Context, input contract.AddTagInput) (contract.TagResponse, error) {
	if err := s.allow("tag.add"); err != nil {
		return contract.TagResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.TagResponse{}, err
	}
	tag, err := s.newTagService().Add(ctx, project, input.EntityType, input.EntityID, input.TagName)
	if err != nil {
		return contract.TagResponse{}, err
	}
	return contract.TagResponse{Project: projectSummary(project), Tag: tagSummary(tag)}, nil
}

func (s *Service) RemoveTag(ctx context.Context, input contract.RemoveTagInput) (contract.RemoveTagResponse, error) {
	if err := s.allow("tag.remove"); err != nil {
		return contract.RemoveTagResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.RemoveTagResponse{}, err
	}
	if !input.Confirmed {
		return contract.RemoveTagResponse{
			Project: projectSummary(project),
			Confirmation: contract.Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a tag is irreversible and requires explicit confirmation.",
				Options:              []contract.ConfirmationOption{{Action: "remove_tag", Label: "Retry with confirmed=true to remove it"}},
			},
		}, nil
	}
	if err := s.newTagService().Remove(ctx, project, input.EntityType, input.EntityID, input.TagID); err != nil {
		return contract.RemoveTagResponse{}, err
	}
	return contract.RemoveTagResponse{Project: projectSummary(project), Removed: true}, nil
}

func (s *Service) ListTags(ctx context.Context, input contract.ListTagsInput) (contract.TagListResponse, error) {
	if err := s.allow("tag.list"); err != nil {
		return contract.TagListResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.TagListResponse{}, err
	}
	tags, err := s.newTagService().List(ctx, project, input.EntityType, input.EntityID)
	if err != nil {
		return contract.TagListResponse{}, err
	}
	return contract.TagListResponse{Project: projectSummary(project), Tags: tagSummaries(tags)}, nil
}

func (s *Service) ListAllTags(ctx context.Context) (contract.AllTagsResponse, error) {
	if err := s.allow("tag.list_all"); err != nil {
		return contract.AllTagsResponse{}, err
	}
	tags, err := s.newTagService().ListAll(ctx)
	if err != nil {
		return contract.AllTagsResponse{}, err
	}
	return contract.AllTagsResponse{Tags: tagSummaries(tags)}, nil
}

func (s *Service) MergeTags(ctx context.Context, input contract.MergeTagsInput) (contract.TagResponse, error) {
	if err := s.allow("tag.merge"); err != nil {
		return contract.TagResponse{}, err
	}
	tag, err := s.newTagService().Merge(ctx, input.SourceTagID, input.TargetTagID)
	if err != nil {
		return contract.TagResponse{}, err
	}
	return contract.TagResponse{Tag: tagSummary(tag)}, nil
}

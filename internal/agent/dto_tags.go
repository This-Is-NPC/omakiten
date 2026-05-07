package agent

import "omakiten/internal/domain"

type TagSummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	UsageCount int    `json:"usage_count,omitempty"`
}

type AddTagInput struct {
	ProjectSelector
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id,omitempty"`
	TagName    string `json:"tag_name"`
}

type TagResponse struct {
	Project ProjectSummary `json:"project"`
	Tag     TagSummary     `json:"tag"`
}

type RemoveTagInput struct {
	ProjectSelector
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id,omitempty"`
	TagID      int64  `json:"tag_id"`
	Confirmed  bool   `json:"confirmed,omitempty"`
}

type RemoveTagResponse struct {
	Project      ProjectSummary `json:"project"`
	Confirmation Confirmation   `json:"confirmation,omitempty"`
	Removed      bool           `json:"removed"`
}

type ListTagsInput struct {
	ProjectSelector
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id,omitempty"`
}

type TagListResponse struct {
	Project ProjectSummary `json:"project"`
	Tags    []TagSummary   `json:"tags"`
}

type AllTagsResponse struct {
	Tags []TagSummary `json:"tags"`
}

type MergeTagsInput struct {
	SourceTagID int64 `json:"source_tag_id"`
	TargetTagID int64 `json:"target_tag_id"`
}

func tagSummary(tag domain.Tag) TagSummary {
	return TagSummary{ID: tag.ID, Name: tag.Name, Label: tag.Label, UsageCount: tag.UsageCount}
}

func tagSummaries(tags []domain.Tag) []TagSummary {
	out := make([]TagSummary, len(tags))
	for i, t := range tags {
		out[i] = tagSummary(t)
	}
	return out
}

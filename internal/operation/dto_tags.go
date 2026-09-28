package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func tagSummary(tag domain.Tag) contract.TagSummary {
	return contract.TagSummary{ID: tag.ID, Name: tag.Name, Label: tag.Label, UsageCount: tag.UsageCount}
}

func tagSummaries(tags []domain.Tag) []contract.TagSummary {
	out := make([]contract.TagSummary, len(tags))
	for i, t := range tags {
		out[i] = tagSummary(t)
	}
	return out
}

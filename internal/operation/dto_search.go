package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func searchHitDTO(h domain.SearchHit) contract.SearchHitDTO {
	return contract.SearchHitDTO{
		EntityType: string(h.EntityType),
		ID:         h.ID,
		Score:      h.Score,
		Snippet:    h.Snippet,
		ProjectID:  h.ProjectID,
	}
}

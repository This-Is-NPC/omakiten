package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func logsRow(row domain.EventRow) contract.LogsRow {
	return contract.LogsRow{
		ID:           row.ID,
		EntityType:   row.EntityType,
		EntityID:     row.EntityID,
		ProjectID:    row.ProjectID,
		ProjectSlug:  row.ProjectSlug,
		EventType:    row.EventType,
		Body:         row.Body,
		Payload:      row.Payload,
		AuthorType:   row.AuthorType,
		Source:       row.Source,
		Status:       row.Status,
		DurationMs:   row.DurationMs,
		ErrorMessage: row.ErrorMessage,
		CreatedAt:    row.CreatedAt,
		FinishedAt:   row.FinishedAt,
		AgentModel:   row.AgentModel,
		Category:     string(row.Category),
		Summary:      row.Summary,
	}
}

func logsRows(rows []domain.EventRow) []contract.LogsRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]contract.LogsRow, len(rows))
	for i, r := range rows {
		out[i] = logsRow(r)
	}
	return out
}

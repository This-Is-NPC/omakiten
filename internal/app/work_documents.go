package app

import (
	"encoding/json"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// WorkDocumentService imports and exports complete work through repository ports.
type WorkDocumentService struct {
	repo DocumentRepository
	snap *config.Snapshot
}

type WorkImportResult struct {
	Plan      *domain.Plan     `json:"plan,omitempty"`
	Tasks     map[string]int64 `json:"tasks,omitempty"`
	TaskCount int              `json:"task_count"`
	WaveCount int              `json:"wave_count"`
	DryRun    bool             `json:"dry_run"`
}

type workMetadata struct {
	Document *domain.WorkDocument      `json:"document,omitempty"`
	Task     domain.WorkTask           `json:"task,omitempty"`
	Waves    map[int64]domain.WorkWave `json:"waves,omitempty"`
}

func NewWorkDocumentService(repo DocumentRepository, snap *config.Snapshot) *WorkDocumentService {
	return &WorkDocumentService{repo: repo, snap: snap}
}

func documentError(message string) error { return domain.NewError(domain.ErrValidation, message, nil) }

func decodeWorkMetadata(data []byte) (workMetadata, error) {
	var metadata workMetadata
	if len(data) != 0 {
		return metadata, json.Unmarshal(data, &metadata)
	}
	return metadata, nil
}

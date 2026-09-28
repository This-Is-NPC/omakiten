package contract

import "omakiten/internal/domain"

// ImportWorkInput carries parsed work without coupling services to file syntax.
type ImportWorkInput struct {
	ProjectSelector
	Document  domain.WorkDocument
	DryRun    bool
	Confirmed bool
}

type ExportWorkInput struct {
	ProjectSelector
	Slug   string
	TaskID int64
}

type ImportWorkResponse struct {
	Project      ProjectSummary   `json:"project"`
	Plan         *domain.Plan     `json:"plan,omitempty"`
	Tasks        map[string]int64 `json:"tasks,omitempty"`
	TaskCount    int              `json:"task_count"`
	WaveCount    int              `json:"wave_count"`
	DryRun       bool             `json:"dry_run"`
	Confirmation Confirmation     `json:"confirmation,omitempty"`
	SimilarTasks []TaskSummary    `json:"similar_tasks,omitempty"`
}

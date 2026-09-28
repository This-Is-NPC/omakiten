package contract

import (
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// BoardSnapshot is the TUI board read-model (D19). Distinct from
// Service.Snapshot (wiring.snapshot) and from the MCP wire DTOs.
type BoardSnapshot struct {
	Tasks        []domain.Task
	Workflow     domain.Workflow
	Dependencies []domain.TaskDependency
	Comments     []domain.Comment
	Laws         []domain.Law
	Skills       []domain.Skill
	Personas     []domain.Persona
	Templates    []config.TaskTemplate
	AllTags      []domain.Tag
	TaskTagsByID map[int64][]domain.Tag
}

// BoardSnapshotInput tunes the TUI snapshot fetch.
type BoardSnapshotInput struct {
	ProjectSelector
	Sort            domain.TaskSort
	IncludeArchived bool
}

type SyncBlockersInput struct {
	ProjectSelector
	TaskID  int64
	TaskIDs []int64
}

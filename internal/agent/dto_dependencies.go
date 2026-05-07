package agent

import "omakiten/internal/domain"

type DependencySummary struct {
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}

type AddDependencyInput struct {
	ProjectSelector
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}

type RemoveDependencyInput struct {
	ProjectSelector
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
	Confirmed       bool  `json:"confirmed,omitempty"`
}

type ListDependenciesInput struct {
	ProjectSelector
	TaskID int64 `json:"task_id,omitempty"`
}

type DependencyResponse struct {
	Project    ProjectSummary    `json:"project"`
	Dependency DependencySummary `json:"dependency"`
}

type RemoveDependencyResponse struct {
	Project      ProjectSummary `json:"project"`
	Confirmation Confirmation   `json:"confirmation,omitempty"`
	Removed      bool           `json:"removed"`
}

type DependenciesResponse struct {
	Project      ProjectSummary      `json:"project"`
	Dependencies []DependencySummary `json:"dependencies"`
}

func dependencySummary(dependency domain.TaskDependency) DependencySummary {
	return DependencySummary{TaskID: dependency.TaskID, DependsOnTaskID: dependency.DependsOnTaskID}
}

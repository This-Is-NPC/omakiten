package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func dependencySummary(dependency domain.TaskDependency) contract.DependencySummary {
	return contract.DependencySummary{TaskID: dependency.TaskID, DependsOnTaskID: dependency.DependsOnTaskID}
}

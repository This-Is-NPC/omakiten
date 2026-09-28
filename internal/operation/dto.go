package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func projectSummary(project domain.ProjectContext) contract.ProjectSummary {
	return contract.ProjectSummary{ID: project.ID, Name: project.Name, Slug: project.Slug, RootPath: project.RootPath}
}

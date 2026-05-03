package domain

type Project struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	RootPath string `json:"root_path"`
}

type ProjectContext struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	RootPath string `json:"root_path"`
}

func (p Project) Context() ProjectContext {
	return ProjectContext(p)
}

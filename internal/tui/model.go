package tui

type Model struct {
	ProjectID int64
	View      string
}

func NewModel(projectID int64) Model {
	return Model{ProjectID: projectID, View: "board"}
}

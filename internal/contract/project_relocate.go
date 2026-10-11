package contract

// RelocateProjectInput updates registration after a directory has moved.
type RelocateProjectInput struct {
	ProjectSelector
	RootPath string `json:"root_path"`
}

// RelocateProjectResponse returns the preserved identity and previous root.
type RelocateProjectResponse struct {
	Project          ProjectSummary `json:"project"`
	PreviousRootPath string         `json:"previous_root_path"`
}

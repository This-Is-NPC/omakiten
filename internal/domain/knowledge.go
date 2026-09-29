package domain

// KnowledgeResource is a document or API operation read from project files.
type KnowledgeResource struct {
	ID          string `json:"id"`
	Project     string `json:"project"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	Path        string `json:"path"`
}

// KnowledgeRelation connects file-backed resources by their qualified IDs.
type KnowledgeRelation struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// KnowledgeSnapshot is rebuilt from source files for each explicit read.
type KnowledgeSnapshot struct {
	Resources   []KnowledgeResource `json:"resources"`
	Relations   []KnowledgeRelation `json:"relations"`
	Diagnostics []string            `json:"diagnostics,omitempty"`
}

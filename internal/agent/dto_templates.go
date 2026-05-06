package agent

// ListTemplatesInput drives the templates.list MCP endpoint. All fields are
// optional; an unfiltered call returns every loaded template without body to
// keep payloads compact.
type ListTemplatesInput struct {
	ProjectSelector
	Kind        string `json:"kind,omitempty"`
	Project     string `json:"project,omitempty"`
	IncludeBody bool   `json:"include_body,omitempty"`
}

type ListTemplatesResponse struct {
	Templates []TemplateSummary `json:"templates"`
}

type ShowTemplateInput struct {
	ProjectSelector
	Slug string `json:"slug"`
}

type ShowTemplateResponse struct {
	Template TemplateSummary `json:"template"`
}

// TemplateSummary is the agent-facing view of a loaded template. Body is
// included on show and optionally on list; default/project surface the active
// binding so the agent can identify the canonical scaffold for a kind.
type TemplateSummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Entity      string `json:"entity,omitempty"`
	Default     string `json:"default,omitempty"`
	Project     string `json:"project,omitempty"`
	IsCustom    bool   `json:"is_custom,omitempty"`
	Body        string `json:"body,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
}

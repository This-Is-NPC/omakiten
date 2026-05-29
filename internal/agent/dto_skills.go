package agent

// ListSkillsInput drives the skills.list MCP endpoint. The endpoint is
// read-only and takes no filters: it returns every loaded skill slug with its
// short description, never the body. Skills are authored by the user — the
// agent never creates, edits, or deletes them through MCP.
type ListSkillsInput struct{}

// ListSkillsResponse carries the catalog of loaded skills without bodies.
type ListSkillsResponse struct {
	Skills []SkillSummary `json:"skills"`
}

// ShowSkillInput identifies one skill by slug for skills.get. Read-only.
type ShowSkillInput struct {
	Slug string `json:"slug"`
}

// ShowSkillResponse returns one resolved skill including its body.
type ShowSkillResponse struct {
	Skill SkillSummary `json:"skill"`
}

// SkillSummary is the agent-facing view of a loaded skill. List omits the
// body (slug + name + description only); show includes it. Skills are
// procedural payloads bound to personas — there is no write path here.
type SkillSummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
}

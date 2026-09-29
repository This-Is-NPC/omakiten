package contract

// ListSkillsInput requests the loaded skill catalog without filters.
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
	Slug        string             `json:"slug"`
	Name        string             `json:"name,omitempty"`
	Description string             `json:"description,omitempty"`
	Body        string             `json:"body,omitempty"`
	Command     *CommandDefinition `json:"command,omitempty"`
}

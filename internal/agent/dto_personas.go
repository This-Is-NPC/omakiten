package agent

// ListPersonasInput drives the personas.list MCP endpoint. Read-only; returns
// every persona wired in the active config personas: block without bodies.
type ListPersonasInput struct{}

// ListPersonasResponse carries active-config personas without bodies.
type ListPersonasResponse struct {
	Personas []PersonaSummary `json:"personas"`
}

// ShowPersonaInput identifies one persona by slug for personas.get. Read-only.
type ShowPersonaInput struct {
	Slug string `json:"slug"`
}

// ShowPersonaResponse returns one persona with body and expanded references.
type ShowPersonaResponse struct {
	Persona PersonaDetail `json:"persona"`
}

// PersonaSummary is the list entry for personas.list — identity only.
type PersonaSummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// PersonaDetail is the personas.get payload: persona body plus every law and
// skill slug declared on the persona, expanded inline with full bodies.
type PersonaDetail struct {
	Slug            string         `json:"slug"`
	Name            string         `json:"name,omitempty"`
	Description     string         `json:"description,omitempty"`
	Body            string         `json:"body,omitempty"`
	Laws            []LawInfo      `json:"laws,omitempty"`
	Skills          []SkillSummary `json:"skills,omitempty"`
	SkillRepertoire []SkillSummary `json:"skill_repertoire,omitempty"`
}

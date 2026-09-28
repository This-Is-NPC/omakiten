package contract

// ListLawsInput drives the laws.list MCP endpoint and CLI `law list`.
// Empty filter fields mean "any". Read-only; bodies omitted.
type ListLawsInput struct {
	Scope   string `json:"scope,omitempty"`
	Project string `json:"project,omitempty"`
	Persona string `json:"persona,omitempty"`
}

// ListLawsResponse carries active-config laws without bodies.
type ListLawsResponse struct {
	Laws []LawSummary `json:"laws"`
}

// ShowLawInput identifies one law by slug for laws.get. Read-only.
type ShowLawInput struct {
	Slug string `json:"slug"`
}

// ShowLawResponse returns one resolved law including its body.
type ShowLawResponse struct {
	Law LawSummary `json:"law"`
}

// LawSummary is the agent-facing view of a loaded law. List omits the body;
// show includes it. Project/Persona mirror config.Law owner slugs for scoped
// laws so list filters can match the prior CLI LawListFilter UX.
type LawSummary struct {
	Slug     string `json:"slug"`
	Name     string `json:"name,omitempty"`
	Severity string `json:"severity,omitempty"`
	Scope    string `json:"scope,omitempty"`
	Project  string `json:"project,omitempty"`
	Persona  string `json:"persona,omitempty"`
	Body     string `json:"body,omitempty"`
}

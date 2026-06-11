package agent

// ListLawsInput drives the laws.list MCP endpoint. Read-only; bodies omitted.
type ListLawsInput struct{}

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
// show includes it.
type LawSummary struct {
	Slug     string `json:"slug"`
	Name     string `json:"name,omitempty"`
	Severity string `json:"severity,omitempty"`
	Scope    string `json:"scope,omitempty"`
	Body     string `json:"body,omitempty"`
}

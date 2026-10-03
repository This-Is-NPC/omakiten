package contract

// EmitEventInput names an external event declared in the project's kit and
// carries its fields: external.<Name>, with Payload as its flat payload.
type EmitEventInput struct {
	ProjectSelector
	Name    string            `json:"name"`
	Payload map[string]string `json:"payload,omitempty"`
}

// EmitEventResponse names the event written; the hooks declared on it run
// in the process that wrote it.
type EmitEventResponse struct {
	Project        ProjectSummary `json:"project"`
	EventType      string         `json:"event_type"`
	NextStepPrompt string         `json:"next_step_prompt"`
}

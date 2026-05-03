package domain

type ContextDump struct {
	Project   ProjectContext `json:"project"`
	Level     int            `json:"level"`
	TaskCount int64          `json:"task_count"`
	Tasks     []Task         `json:"tasks,omitempty"`
	Laws      []Law          `json:"laws,omitempty"`
}

type Law struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Severity string `json:"severity"`
	Body     string `json:"body"`
}

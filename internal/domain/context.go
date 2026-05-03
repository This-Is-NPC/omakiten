package domain

type ContextDump struct {
	Project        ProjectContext   `json:"project"`
	Level          int              `json:"level"`
	TaskCount      int64            `json:"task_count"`
	TokenMetrics   TokenMetrics     `json:"token_metrics"`
	ContextEntries []ContextEntry   `json:"context_entries,omitempty"`
	Workflow       Workflow         `json:"workflow,omitempty"`
	Tasks          []Task           `json:"tasks,omitempty"`
	Dependencies   []TaskDependency `json:"dependencies,omitempty"`
	Comments       []Comment        `json:"comments,omitempty"`
	Laws           []Law            `json:"laws,omitempty"`
}

type Law struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Severity string `json:"severity"`
	Body     string `json:"body"`
}

type ContextEntry struct {
	ID            int64  `json:"id"`
	ProjectID     int64  `json:"project_id"`
	Body          string `json:"body"`
	TokenEstimate int    `json:"token_estimate"`
	CreatedAt     string `json:"created_at,omitempty"`
}

type TokenMetrics struct {
	EstimatedTotal int  `json:"estimated_total"`
	MaxTokens      int  `json:"max_tokens"`
	Truncated      bool `json:"truncated,omitempty"`
}

type ContextSettings struct {
	DefaultLevel int `json:"default_level"`
	MaxTokens    int `json:"max_tokens"`
}

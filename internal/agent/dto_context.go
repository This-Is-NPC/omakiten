package agent

import "omakiten/internal/domain"

type ContextSnippet struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at,omitempty"`
}

type AddContextInput struct {
	ProjectSelector
	Body string `json:"body"`
}

type ContextResponse struct {
	Project ProjectSummary `json:"project"`
	Entry   ContextSnippet `json:"context_entry"`
}

type DumpContextInput struct {
	ProjectSelector
	Level int `json:"level,omitempty"`
}

type DumpContextResponse struct {
	Project      ProjectSummary      `json:"project"`
	Level        int                 `json:"level"`
	TaskCount    int64               `json:"task_count"`
	TokenMetrics domain.TokenMetrics `json:"token_metrics"`
	Context      []ContextSnippet    `json:"context_entries,omitempty"`
	Workflow     WorkflowSummary     `json:"workflow,omitempty"`
	Tasks        []TaskSummary       `json:"tasks,omitempty"`
	Dependencies []DependencySummary `json:"dependencies,omitempty"`
	Comments     []CommentSummary    `json:"comments,omitempty"`
}

func contextSnippet(entry domain.ContextEntry) ContextSnippet {
	return ContextSnippet{ID: entry.ID, Body: entry.Body, CreatedAt: entry.CreatedAt}
}

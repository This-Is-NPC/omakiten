package domain

type Tag struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	UsageCount int    `json:"usage_count,omitempty"`
}

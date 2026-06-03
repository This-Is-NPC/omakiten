package domain

type TokenMetrics struct {
	EstimatedTotal int  `json:"estimated_total"`
	MaxTokens      int  `json:"max_tokens"`
	Truncated      bool `json:"truncated,omitempty"`
}

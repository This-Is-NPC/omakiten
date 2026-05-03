package token

import "strings"

type Counter interface {
	Count(text string) int
}

type ApproxCounter struct{}

func (ApproxCounter) Count(text string) int {
	words := strings.Fields(text)
	if len(words) == 0 {
		return 0
	}
	return len(words)
}

package token

import (
	"strings"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

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

type BPECounter struct {
	encoding *tiktoken.Tiktoken
}

func NewCounter() Counter {
	counter, err := NewBPECounter()
	if err != nil {
		return ApproxCounter{}
	}
	return counter
}

func NewBPECounter() (BPECounter, error) {
	encoding, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return BPECounter{}, err
	}
	return BPECounter{encoding: encoding}, nil
}

func (c BPECounter) Count(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	if c.encoding == nil {
		return ApproxCounter{}.Count(text)
	}
	return len(c.encoding.Encode(text, nil, nil))
}

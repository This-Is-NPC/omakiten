package token

import (
	"errors"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestApproxCounter(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "whitespace only", text: "   \t\n", want: 0},
		{name: "single word", text: "hello", want: 1},
		{name: "multiple words", text: "the quick brown fox", want: 4},
	}

	counter := ApproxCounter{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := counter.Count(tt.text); got != tt.want {
				t.Fatalf("ApproxCounter.Count(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestNewCounterDoesNotAccessNetwork(t *testing.T) {
	t.Setenv("TIKTOKEN_CACHE_DIR", t.TempDir())

	requests := 0
	oldTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("network access blocked by test")
	})
	t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })

	counter := NewCounter()
	if requests != 0 {
		t.Fatalf("NewCounter() made %d HTTP request(s), want 0", requests)
	}
	if got := counter.Count("hello world"); got != 2 {
		t.Fatalf("NewCounter().Count(hello world) = %d, want 2", got)
	}
}

func TestNewCounterReturnsUsableCounter(t *testing.T) {
	counter := NewCounter()
	if counter == nil {
		t.Fatalf("NewCounter() = nil, want non-nil counter")
	}
	if got := counter.Count("hello world"); got <= 0 {
		t.Fatalf("NewCounter().Count(hello world) = %d, want > 0", got)
	}
}

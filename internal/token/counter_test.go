package token

import "testing"

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

func TestBPECounterReturnsPositiveCounts(t *testing.T) {
	counter, err := NewBPECounter()
	if err != nil {
		t.Skipf("BPE encoding unavailable: %v", err)
	}

	if got := counter.Count(""); got != 0 {
		t.Fatalf("BPECounter.Count(empty) = %d, want 0", got)
	}
	if got := counter.Count("   "); got != 0 {
		t.Fatalf("BPECounter.Count(whitespace) = %d, want 0", got)
	}
	if got := counter.Count("hello world"); got <= 0 {
		t.Fatalf("BPECounter.Count(hello world) = %d, want > 0", got)
	}
	long := counter.Count("the quick brown fox jumps over the lazy dog")
	short := counter.Count("hi")
	if long <= short {
		t.Fatalf("BPECounter.Count(long) = %d, BPECounter.Count(short) = %d, want long > short", long, short)
	}
}

func TestBPECounterFallsBackWhenEncodingMissing(t *testing.T) {
	counter := BPECounter{}
	if got := counter.Count("the quick brown fox"); got != 4 {
		t.Fatalf("BPECounter{}.Count() = %d, want approximate fallback (4)", got)
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

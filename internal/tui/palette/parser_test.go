package palette

import (
	"errors"
	"testing"
)

func TestParseAcceptsNav(t *testing.T) {
	tok, err := Parse("nav:31")
	if err != nil {
		t.Fatalf("Parse(nav:31) error = %v", err)
	}
	if tok.Verb != "nav" || tok.Operand != "31" || tok.Raw != "nav:31" {
		t.Fatalf("Parse(nav:31) = %+v, want {nav 31 nav:31}", tok)
	}
}

func TestParseAcceptsOp(t *testing.T) {
	tok, err := Parse("op:381")
	if err != nil {
		t.Fatalf("Parse(op:381) error = %v", err)
	}
	if tok.Verb != "op" || tok.Operand != "381" {
		t.Fatalf("Parse(op:381) = %+v", tok)
	}
}

func TestParseAcceptsUserDefinedVerb(t *testing.T) {
	cases := []struct {
		input   string
		verb    string
		operand string
	}{
		{"hook:1", "hook", "1"},
		{"cmd:standup", "cmd", "standup"},
		{"kata-2:run", "kata-2", "run"},
		{"k1:value", "k1", "value"},
		{"under_score:x", "under_score", "x"},
	}
	for _, c := range cases {
		tok, err := Parse(c.input)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", c.input, err)
			continue
		}
		if tok.Verb != c.verb || tok.Operand != c.operand {
			t.Errorf("Parse(%q) = %+v, want verb=%q operand=%q", c.input, tok, c.verb, c.operand)
		}
	}
}

func TestParseTrimsWhitespace(t *testing.T) {
	tok, err := Parse("  nav : 31  ")
	if err != nil {
		t.Fatalf("Parse(padded) error = %v", err)
	}
	if tok.Verb != "nav" || tok.Operand != "31" {
		t.Fatalf("Parse(padded) = %+v", tok)
	}
}

func TestParseRejectsMissingColon(t *testing.T) {
	_, err := Parse("nav31")
	if !errors.Is(err, ErrMissingColon) {
		t.Fatalf("Parse(nav31) error = %v, want ErrMissingColon", err)
	}
}

func TestParseRejectsTooManyColons(t *testing.T) {
	_, err := Parse("nav:31:foo")
	if !errors.Is(err, ErrTooManyColons) {
		t.Fatalf("Parse(nav:31:foo) error = %v, want ErrTooManyColons", err)
	}
}

func TestParseRejectsEmptyVerb(t *testing.T) {
	_, err := Parse(":31")
	if !errors.Is(err, ErrEmptyVerb) {
		t.Fatalf("Parse(:31) error = %v, want ErrEmptyVerb", err)
	}
}

func TestParseRejectsEmptyOperand(t *testing.T) {
	_, err := Parse("nav:")
	if !errors.Is(err, ErrEmptyOperand) {
		t.Fatalf("Parse(nav:) error = %v, want ErrEmptyOperand", err)
	}
}

func TestParseRejectsUppercaseVerb(t *testing.T) {
	_, err := Parse("Nav:31")
	if !errors.Is(err, ErrInvalidVerb) {
		t.Fatalf("Parse(Nav:31) error = %v, want ErrInvalidVerb", err)
	}
}

func TestParseRejectsVerbStartingWithDigit(t *testing.T) {
	_, err := Parse("1nav:31")
	if !errors.Is(err, ErrInvalidVerb) {
		t.Fatalf("Parse(1nav:31) error = %v, want ErrInvalidVerb", err)
	}
}

func TestParseRejectsVerbWithSpace(t *testing.T) {
	_, err := Parse("na v:31")
	if !errors.Is(err, ErrInvalidVerb) {
		t.Fatalf("Parse(na v:31) error = %v, want ErrInvalidVerb", err)
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	_, err := Parse("")
	if !errors.Is(err, ErrMissingColon) {
		t.Fatalf("Parse(empty) error = %v, want ErrMissingColon", err)
	}
	_, err = Parse("   ")
	if !errors.Is(err, ErrMissingColon) {
		t.Fatalf("Parse(spaces) error = %v, want ErrMissingColon", err)
	}
}

package palette

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Token is a parsed trick submission. Raw preserves the user's
// original input (post-trim) so downstream consumers — handlers,
// hook payloads — can echo the typed form back verbatim without
// having to re-render verb+operand themselves.
type Token struct {
	Verb    string
	Operand string
	Raw     string
}

// ParseError categorises malformed input so the caller can pick the
// right i18n status key. The error chain is preserved (errors.Is)
// so callers that only care that parsing failed still get a clean
// boolean check.
type ParseError struct {
	Reason string
}

func (e *ParseError) Error() string { return e.Reason }

// ErrMissingColon, ErrEmptyVerb, ErrEmptyOperand, ErrInvalidVerb are
// the leaf sentinels so callers can map each to a distinct i18n
// status without parsing the error message.
var (
	ErrMissingColon  = errors.New("trick input must contain a verb:operand separator")
	ErrTooManyColons = errors.New("trick input must contain exactly one : separator")
	ErrEmptyVerb     = errors.New("trick verb must be non-empty")
	ErrEmptyOperand  = errors.New("trick operand must be non-empty")
	ErrInvalidVerb   = errors.New("trick verb must match [a-z][a-z0-9_-]*")
)

// verbPattern enforces the verb grammar: lowercase ASCII, starts
// with a letter, may contain digits / underscore / hyphen after.
// Strict so user-defined verbs render predictably across locales
// and so type-prefix grammar reserved for `op:` (`op:t<id>` etc.)
// has unambiguous parse rules later.
var verbPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Parse splits a raw palette input into a Token. The grammar is
// `verb:operand` with exactly one `:` separator; both sides are
// trimmed of surrounding whitespace before validation so paste-
// from-clipboard noise does not surface as a malformed-input
// error. Operand grammar is verb-specific and validated by the
// downstream handler (nav requires positional digits; op requires
// numeric id at MVP); the parser only enforces non-empty.
func Parse(raw string) (Token, error) {
	trimmed := strings.TrimSpace(raw)
	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return Token{}, ErrMissingColon
	}
	if strings.Count(trimmed, ":") > 1 {
		return Token{}, ErrTooManyColons
	}
	verb := strings.TrimSpace(trimmed[:idx])
	operand := strings.TrimSpace(trimmed[idx+1:])
	if verb == "" {
		return Token{}, ErrEmptyVerb
	}
	if operand == "" {
		return Token{}, ErrEmptyOperand
	}
	if !verbPattern.MatchString(verb) {
		return Token{}, fmt.Errorf("%w: %q", ErrInvalidVerb, verb)
	}
	return Token{Verb: verb, Operand: operand, Raw: trimmed}, nil
}

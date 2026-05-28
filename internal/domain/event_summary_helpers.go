package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- payload helpers ---------------------------------------------------

// decodePayload parses an event's JSON payload into a map. Returns a
// nil map (not an error) on any failure — callers treat absent fields
// as zero-value, and SummarizeEvent never panics on malformed JSON.
func decodePayload(payload string) map[string]any {
	if payload == "" {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return nil
	}
	return out
}

func readString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// JSON numbers decode as float64; render integers without a
		// trailing decimal when the value is integral.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func readInt(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

func readBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func readObject(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func readStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// readBodyDelta pulls the {from,to} body delta written by the
// comment.edited emitter. Either side may be absent — the caller
// renders whichever side it has.
func readBodyDelta(m map[string]any) (from, to string) {
	body := readObject(m, "body")
	if body == nil {
		return "", ""
	}
	return readString(body, "from"), readString(body, "to")
}

// condenseLine collapses runs of whitespace (including newlines) into
// single spaces and trims edges. Used on free-form bodies and titles
// so the Logs grid stays one row per event.
func condenseLine(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// condenseJSON re-encodes a JSON payload with no extra whitespace.
// Returns (condensed, true) on success; (raw, false) when the input
// is not valid JSON. Used by the unknown-event fallback so payloads
// land on a single row regardless of how they were stored.
func condenseJSON(payload string) (string, bool) {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return "", false
	}
	var any any
	if err := json.Unmarshal([]byte(trimmed), &any); err != nil {
		return "", false
	}
	out, err := json.Marshal(any)
	if err != nil {
		return "", false
	}
	return string(out), true
}

func fallback(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

// strSliceSort is a tiny in-place sort that avoids dragging sort into
// the import set just for one rendering helper. Linear in the slice
// length given the field maps we render fit comfortably in O(n^2).
func strSliceSort(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}

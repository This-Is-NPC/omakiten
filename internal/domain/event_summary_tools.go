package domain

import (
	"fmt"
	"strings"
)

func init() {
	registerFormatter(EventTypeCLIToolCall, summarizeToolCall)
	registerFormatter(EventTypeMCPToolCall, summarizeToolCall)
	registerFormatter(EventTypeTUIToolCall, summarizeToolCall)
}

// summarizeToolCall renders the per-invocation activity log entries
// shared by CLI / MCP / TUI tool surfaces. The three event_types share
// the same payload shape so a single arm covers all three.
func summarizeToolCall(row EventRow) string {
	payload := decodePayload(row.Payload)
	tool := readString(payload, "tool_name")
	source := readString(payload, "source")
	if source == "" {
		source = strings.TrimSpace(row.Source)
	}
	status := readString(payload, "status")
	if status == "" {
		status = strings.TrimSpace(row.Status)
	}
	dur, _ := readInt(payload, "duration_ms")
	if dur == 0 && row.DurationMs != 0 {
		dur = row.DurationMs
	}
	header := tool
	if source != "" {
		if header != "" {
			header = source + "/" + header
		} else {
			header = source
		}
	}
	if header == "" {
		header = "tool_call"
	}
	parts := []string{header}
	if status != "" {
		parts = append(parts, "["+status+"]")
	}
	if dur > 0 {
		parts = append(parts, fmt.Sprintf("%dms", dur))
	}
	return strings.Join(parts, " ")
}

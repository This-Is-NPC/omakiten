package domain

import "fmt"

func init() {
	registerFormatter(EventTypeUpdateHealthCheckPassed, summarizeUpdateHealthCheckPassed)
	registerFormatter(EventTypeUpdateHealthCheckFailed, summarizeUpdateHealthCheckFailed)
	registerFormatter(EventTypeUpdateSwapCompleted, summarizeUpdateSwapCompleted)
	registerFormatter(EventTypeUpdateSwapAborted, summarizeUpdateSwapAborted)
	registerFormatter(EventTypeTUIHealthCheckFailed, summarizeTUIHealthCheckFailed)
}

func summarizeUpdateHealthCheckPassed(row EventRow) string {
	payload := decodePayload(row.Payload)
	from := readString(payload, "from_version")
	to := readString(payload, "to_version")
	if from != "" && to != "" {
		return fmt.Sprintf("update health-check passed %s → %s", from, to)
	}
	return "update health-check passed"
}

func summarizeUpdateHealthCheckFailed(row EventRow) string {
	payload := decodePayload(row.Payload)
	kind := readString(payload, "validator_first_error_kind")
	count, _ := readInt(payload, "validator_error_count")
	if kind != "" {
		return fmt.Sprintf("update health-check failed (%d errors, first: %s)", count, kind)
	}
	if count > 0 {
		return fmt.Sprintf("update health-check failed (%d errors)", count)
	}
	return "update health-check failed"
}

func summarizeUpdateSwapCompleted(row EventRow) string {
	payload := decodePayload(row.Payload)
	from := readString(payload, "from_version")
	to := readString(payload, "to_version")
	if from != "" && to != "" {
		return fmt.Sprintf("update binary swap %s → %s", from, to)
	}
	return "update binary swap completed"
}

func summarizeUpdateSwapAborted(row EventRow) string {
	payload := decodePayload(row.Payload)
	reason := readString(payload, "reason")
	if reason != "" {
		return fmt.Sprintf("update binary swap aborted (%s)", reason)
	}
	return "update binary swap aborted"
}

func summarizeTUIHealthCheckFailed(row EventRow) string {
	payload := decodePayload(row.Payload)
	kind := readString(payload, "validator_first_error_kind")
	if kind != "" {
		return fmt.Sprintf("tui boot health-check failed (%s)", kind)
	}
	return "tui boot health-check failed"
}

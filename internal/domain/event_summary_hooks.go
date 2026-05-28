package domain

import (
	"fmt"
)

func init() {
	registerFormatter(EventTypeHookExecuted, summarizeHookExecuted)
	registerFormatter(EventTypeSubtaskKitNoticeEmitted, summarizeSubtaskKitNoticeEmitted)
	registerFormatter(EventTypeBundleSwapped, summarizeBundleSwapped)
	registerFormatter(EventTypeBundleImported, summarizeBundleImported)
}

func summarizeHookExecuted(row EventRow) string {
	payload := decodePayload(row.Payload)
	action := readString(payload, "action")
	ev := readString(payload, "event_type")
	success := readBool(payload, "success")
	var status string
	if _, ok := payload["success"]; ok {
		if success {
			status = "ok"
		} else {
			status = "fail"
		}
	}
	switch {
	case action != "" && ev != "" && status != "":
		return fmt.Sprintf("hook %s on %s [%s]", action, ev, status)
	case action != "" && ev != "":
		return fmt.Sprintf("hook %s on %s", action, ev)
	case action != "":
		return "hook " + action
	case ev != "":
		return "hook on " + ev
	}
	return "hook executed"
}

func summarizeSubtaskKitNoticeEmitted(row EventRow) string {
	payload := decodePayload(row.Payload)
	from := readString(payload, "from_kit")
	to := readString(payload, "to_kit")
	if from != "" || to != "" {
		return fmt.Sprintf("subtask kit %s → %s", fallback(from, "(none)"), fallback(to, "(none)"))
	}
	if key := readString(payload, "i18n_key"); key != "" {
		return "subtask kit notice: " + key
	}
	return "subtask kit notice"
}

func summarizeBundleSwapped(row EventRow) string {
	payload := decodePayload(row.Payload)
	from := readString(payload, "from_workflow")
	to := readString(payload, "to_workflow")
	orphans, _ := readInt(payload, "orphan_count")
	base := fmt.Sprintf("bundle %s → %s", fallback(from, "?"), fallback(to, "?"))
	if orphans > 0 {
		base += fmt.Sprintf(" (%d orphan(s))", orphans)
	}
	return base
}

func summarizeBundleImported(row EventRow) string {
	payload := decodePayload(row.Payload)
	wf := readString(payload, "workflow_key")
	hash := readString(payload, "hash")
	if wf != "" && hash != "" {
		return fmt.Sprintf("bundle imported workflow=%s hash=%s", wf, condenseLine(hash))
	}
	if wf != "" {
		return "bundle imported workflow=" + wf
	}
	return "bundle imported"
}

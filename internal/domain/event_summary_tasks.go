package domain

import (
	"fmt"
	"strings"
)

func init() {
	register(EventTypeTaskCreated, summarizeTaskCreated)
	register(EventTypeTaskMoved, summarizeTaskMoved)
	register(EventTypeTaskMigrated, summarizeTaskMigrated)
	register(EventTypeTaskBucketOrphaned, summarizeTaskBucketOrphaned)
	register(EventTypeTaskCompleted, summarizeTaskCompleted)
	register(EventTypeTaskEdited, summarizeTaskEdited)
	register(EventTypeTaskRemoved, summarizeTaskRemoved)
	register(EventTypeTaskArchived, summarizeTaskArchived)
	register(EventTypeTaskUnarchived, summarizeTaskUnarchived)
	register(EventTypeTaskAssigned, summarizeTaskAssigned)
	register(EventTypeTaskUnassigned, summarizeTaskUnassigned)
}

func summarizeTaskCreated(row EventRow) string {
	payload := decodePayload(row.Payload)
	title := readString(payload, "title")
	bucket := readString(payload, "bucket")
	priority := readString(payload, "priority")
	parts := []string{}
	if title != "" {
		parts = append(parts, fmt.Sprintf("%q", condenseLine(title)))
	}
	if bucket != "" {
		parts = append(parts, "→ "+bucket)
	}
	if priority != "" {
		parts = append(parts, "["+priority+"]")
	}
	if len(parts) == 0 {
		return "task created"
	}
	return "created " + strings.Join(parts, " ")
}

func summarizeTaskMoved(row EventRow) string {
	payload := decodePayload(row.Payload)
	from := readString(payload, "from")
	to := readString(payload, "to")
	if from != "" || to != "" {
		return fmt.Sprintf("moved %s → %s", fallback(from, "?"), fallback(to, "?"))
	}
	return "task moved"
}

func summarizeTaskMigrated(row EventRow) string {
	payload := decodePayload(row.Payload)
	from := readString(payload, "from")
	to := readString(payload, "to")
	reason := readString(payload, "reason")
	base := fmt.Sprintf("migrated %s → %s", fallback(from, "?"), fallback(to, "?"))
	if reason != "" {
		base += " (" + reason + ")"
	}
	return base
}

func summarizeTaskBucketOrphaned(row EventRow) string {
	payload := decodePayload(row.Payload)
	old := readString(payload, "old_bucket")
	toKit := readString(payload, "to_kit")
	base := "bucket orphaned"
	if old != "" {
		base += " from " + old
	}
	if toKit != "" {
		base += " (kit=" + toKit + ")"
	}
	return base
}

func summarizeTaskCompleted(row EventRow) string {
	payload := decodePayload(row.Payload)
	if bucket := readString(payload, "bucket"); bucket != "" {
		return "completed → " + bucket
	}
	return "task completed"
}

func summarizeTaskEdited(row EventRow) string {
	payload := decodePayload(row.Payload)
	fields := readObject(payload, "fields")
	if len(fields) == 0 {
		return "task edited"
	}
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	// Stable order for deterministic rendering.
	strSliceSort(names)
	return "edited " + strings.Join(names, ", ")
}

func summarizeTaskRemoved(row EventRow) string {
	payload := decodePayload(row.Payload)
	title := readString(payload, "title")
	if title != "" {
		return fmt.Sprintf("removed %q", condenseLine(title))
	}
	return "task removed"
}

func summarizeTaskArchived(row EventRow) string {
	payload := decodePayload(row.Payload)
	if bucket := readString(payload, "bucket"); bucket != "" {
		return "archived from " + bucket
	}
	return "task archived"
}

func summarizeTaskUnarchived(row EventRow) string {
	payload := decodePayload(row.Payload)
	if bucket := readString(payload, "bucket"); bucket != "" {
		return "unarchived → " + bucket
	}
	return "task unarchived"
}

func summarizeTaskAssigned(row EventRow) string {
	payload := decodePayload(row.Payload)
	assignee := readString(payload, "assignee")
	source := readString(payload, "source")
	if assignee != "" && source != "" {
		return fmt.Sprintf("assigned to %s via %s", assignee, source)
	}
	if assignee != "" {
		return "assigned to " + assignee
	}
	return "task assigned"
}

func summarizeTaskUnassigned(row EventRow) string {
	payload := decodePayload(row.Payload)
	if former := readString(payload, "former_assignee"); former != "" {
		return "unassigned " + former
	}
	return "task unassigned"
}

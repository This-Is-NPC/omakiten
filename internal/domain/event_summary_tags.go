package domain

import (
	"fmt"
)

func init() {
	register(EventTypeTagAdded, summarizeTagAdded)
	register(EventTypeTagRemoved, summarizeTagRemoved)
	register(EventTypeDependencyAdded, summarizeDependencyAdded)
	register(EventTypeDependencyRemoved, summarizeDependencyRemoved)
}

func summarizeTagAdded(row EventRow) string {
	payload := decodePayload(row.Payload)
	tag := readString(payload, "tag_name")
	ent := readString(payload, "entity_type")
	if tag != "" && ent != "" {
		return fmt.Sprintf("tag +%s on %s", tag, ent)
	}
	if tag != "" {
		return "tag +" + tag
	}
	return "tag added"
}

func summarizeTagRemoved(row EventRow) string {
	payload := decodePayload(row.Payload)
	tag := readString(payload, "tag_name")
	ent := readString(payload, "entity_type")
	if tag != "" && ent != "" {
		return fmt.Sprintf("tag -%s on %s", tag, ent)
	}
	if tag != "" {
		return "tag -" + tag
	}
	return "tag removed"
}

func summarizeDependencyAdded(row EventRow) string {
	payload := decodePayload(row.Payload)
	if id, ok := readInt(payload, "depends_on_task_id"); ok {
		return fmt.Sprintf("depends on #%d", id)
	}
	return "dependency added"
}

func summarizeDependencyRemoved(row EventRow) string {
	payload := decodePayload(row.Payload)
	if id, ok := readInt(payload, "depends_on_task_id"); ok {
		return fmt.Sprintf("dropped dep on #%d", id)
	}
	return "dependency removed"
}

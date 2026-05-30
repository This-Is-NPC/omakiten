package domain

import (
	"fmt"
	"strings"
)

func init() {
	registerFormatter(EventTypeComment, summarizeComment)
	registerFormatter(EventTypeCommentEdited, summarizeCommentEdited)
	registerFormatter(EventTypeCommentRemoved, summarizeCommentRemoved)
}

func summarizeComment(row EventRow) string {
	who := strings.TrimSpace(row.AuthorType)
	body := condenseLine(row.Body)
	switch {
	case who != "" && body != "":
		return fmt.Sprintf("%s: %s", who, body)
	case body != "":
		return body
	case who != "":
		return who + ": (empty)"
	default:
		return "comment"
	}
}

func summarizeCommentEdited(row EventRow) string {
	payload := decodePayload(row.Payload)

	// Prefer the body delta when present; otherwise name whichever metadata
	// field changed (pin/title/kind) so the feed distinguishes them instead of
	// rendering a content-free "edited comment #N".
	if from, to, ok := readDelta(payload, "body"); ok && (from != "" || to != "") {
		return fmt.Sprintf("edited: %q → %q", condenseLine(from), condenseLine(to))
	}
	if from, to, ok := readDelta(payload, "pinned"); ok {
		if to == "true" {
			return "pinned"
		}
		if from == "true" {
			return "unpinned"
		}
	}
	if from, to, ok := readDelta(payload, "title"); ok {
		return fmt.Sprintf("retitled: %q → %q", condenseLine(from), condenseLine(to))
	}
	if from, to, ok := readDelta(payload, "kind"); ok {
		return fmt.Sprintf("kind: %s → %s", fallback(condenseLine(from), "none"), fallback(condenseLine(to), "none"))
	}
	if id, ok := readInt(payload, "comment_id"); ok {
		return fmt.Sprintf("edited comment #%d", id)
	}
	return "comment edited"
}

func summarizeCommentRemoved(row EventRow) string {
	payload := decodePayload(row.Payload)
	body := readString(payload, "body")
	if body != "" {
		return fmt.Sprintf("removed: %q", condenseLine(body))
	}
	if id, ok := readInt(payload, "comment_id"); ok {
		return fmt.Sprintf("removed comment #%d", id)
	}
	return "comment removed"
}

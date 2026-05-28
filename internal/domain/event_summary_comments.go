package domain

import (
	"fmt"
	"strings"
)

func init() {
	register(EventTypeComment, summarizeComment)
	registerFormatter(EventTypeComment, summarizeComment)
	register(EventTypeCommentEdited, summarizeCommentEdited)
	registerFormatter(EventTypeCommentEdited, summarizeCommentEdited)
	register(EventTypeCommentRemoved, summarizeCommentRemoved)
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
	from, to := readBodyDelta(payload)
	if from != "" || to != "" {
		return fmt.Sprintf("edited: %q → %q", condenseLine(from), condenseLine(to))
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

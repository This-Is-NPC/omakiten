package domain

import (
	"fmt"
	"strings"
)

func init() {
	registerFormatter(EventTypeProjectRemoved, summarizeProjectRemoved)
	registerFormatter(EventTypeConfirmationGranted, summarizeConfirmationGranted)
	registerFormatter(EventTypeErrorRecorded, summarizeErrorRecorded)
	registerFormatter(EventTypeErrorsResearched, summarizeErrorsResearched)
	registerFormatter(EventTypeSolutionAdded, summarizeSolutionAdded)
	registerFormatter(EventTypeSolutionConfirmed, summarizeSolutionConfirmed)
	registerFormatter(EventTypeSolutionLiked, summarizeSolutionLiked)
	registerFormatter(EventTypeSolutionFailed, summarizeSolutionFailed)
	registerFormatter(EventTypeSolutionViewedTop, summarizeSolutionViewedTop)
}

func summarizeProjectRemoved(row EventRow) string {
	payload := decodePayload(row.Payload)
	slug := readString(payload, "slug")
	name := readString(payload, "name")
	switch {
	case slug != "" && name != "":
		return fmt.Sprintf("removed project %s (%s)", slug, condenseLine(name))
	case slug != "":
		return "removed project " + slug
	case name != "":
		return "removed project " + condenseLine(name)
	}
	return "project removed"
}

func summarizeConfirmationGranted(row EventRow) string {
	payload := decodePayload(row.Payload)
	slug := readString(payload, "notification_slug")
	cmd := readString(payload, "command")
	if slug != "" && cmd != "" {
		return fmt.Sprintf("confirmed %s: %s", slug, condenseLine(cmd))
	}
	if slug != "" {
		return "confirmed " + slug
	}
	if cmd != "" {
		return "confirmed command: " + condenseLine(cmd)
	}
	return "confirmation granted"
}

func summarizeErrorRecorded(row EventRow) string {
	payload := decodePayload(row.Payload)
	tags := readStringSlice(payload, "tags")
	hasCtx := readBool(payload, "has_context")
	base := "error recorded"
	if len(tags) > 0 {
		base += " #" + strings.Join(tags, " #")
	}
	if hasCtx {
		base += " (+context)"
	}
	return base
}

func summarizeErrorsResearched(row EventRow) string {
	payload := decodePayload(row.Payload)
	q := readString(payload, "query")
	count, hasCount := readInt(payload, "result_count")
	switch {
	case q != "" && hasCount:
		return fmt.Sprintf("researched %q → %d hit(s)", condenseLine(q), count)
	case q != "":
		return fmt.Sprintf("researched %q", condenseLine(q))
	case hasCount:
		return fmt.Sprintf("research → %d hit(s)", count)
	}
	return "errors researched"
}

func summarizeSolutionAdded(row EventRow) string {
	payload := decodePayload(row.Payload)
	if id, ok := readInt(payload, "error_id"); ok {
		return fmt.Sprintf("solution added for error #%d", id)
	}
	return "solution added"
}

func summarizeSolutionConfirmed(row EventRow) string {
	payload := decodePayload(row.Payload)
	id, _ := readInt(payload, "error_id")
	success := readBool(payload, "success")
	outcome := "fail"
	if success {
		outcome = "ok"
	}
	if id != 0 {
		return fmt.Sprintf("solution confirmed [%s] for error #%d", outcome, id)
	}
	return "solution confirmed [" + outcome + "]"
}

func summarizeSolutionLiked(row EventRow) string {
	payload := decodePayload(row.Payload)
	id, _ := readInt(payload, "error_id")
	likes, _ := readInt(payload, "likes")
	switch {
	case id != 0 && likes != 0:
		return fmt.Sprintf("solution liked (error #%d, %d like(s))", id, likes)
	case id != 0:
		return fmt.Sprintf("solution liked (error #%d)", id)
	case likes != 0:
		return fmt.Sprintf("solution liked (%d like(s))", likes)
	}
	return "solution liked"
}

func summarizeSolutionFailed(row EventRow) string {
	payload := decodePayload(row.Payload)
	if id, ok := readInt(payload, "error_id"); ok {
		return fmt.Sprintf("solution failed (error #%d)", id)
	}
	return "solution failed"
}

func summarizeSolutionViewedTop(row EventRow) string {
	payload := decodePayload(row.Payload)
	limit, hasL := readInt(payload, "limit")
	count, hasC := readInt(payload, "returned_count")
	switch {
	case hasL && hasC:
		return fmt.Sprintf("top solutions viewed (%d/%d)", count, limit)
	case hasL:
		return fmt.Sprintf("top solutions viewed (limit %d)", limit)
	case hasC:
		return fmt.Sprintf("top solutions viewed (%d row(s))", count)
	}
	return "top solutions viewed"
}

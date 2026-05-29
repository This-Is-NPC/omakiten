package domain

import (
	"fmt"
	"strings"
)

func init() {
	registerFormatter(EventTypeNoteCreated, summarizeNoteCreated)
	registerFormatter(EventTypeNoteEdited, summarizeNoteEdited)
	registerFormatter(EventTypeNotePinned, summarizeNotePinned)
	registerFormatter(EventTypeNoteRemoved, summarizeNoteRemoved)
}

// summarizeNoteCreated renders "note created <title> (kind · scope) #tags".
// Mirrors summarizeTaskCreated so the activity feed reads consistently
// across entity types — verb-first, quoted subject, metadata in parens,
// tag chips appended.
func summarizeNoteCreated(row EventRow) string {
	return renderNoteSummary(row, "note created")
}

// summarizeNoteEdited renders "note edited <title> (kind · scope) #tags".
// Tags reflect the post-mutation set so a tag-replacement edit shows the
// new set rather than the previous one.
func summarizeNoteEdited(row EventRow) string {
	return renderNoteSummary(row, "note edited")
}

// summarizeNotePinned distinguishes pin from unpin via the `pinned`
// payload key. UpdateNote always co-emits this with note.edited so the
// summary stays specific to the toggle ("pinned" / "unpinned") and lets
// the edit line carry the broader change context.
func summarizeNotePinned(row EventRow) string {
	payload := decodePayload(row.Payload)
	verb := "note pinned"
	if !readBool(payload, "pinned") {
		verb = "note unpinned"
	}
	return renderNoteSummaryFromPayload(payload, verb)
}

// summarizeNoteRemoved relies on the title snapshot stored in the payload
// before the row is hard-deleted. Without the snapshot the formatter has
// no way to label the lost row, so the bare verb fallback documents the
// payload-shape contract directly.
func summarizeNoteRemoved(row EventRow) string {
	return renderNoteSummary(row, "note removed")
}

// renderNoteSummary is the shared rendering shape for the three
// title-carrying note formatters. The verb doubles as the fallback
// when the payload yields no meaningful chunks, guaranteeing a
// non-empty single-line output per SummarizeEvent's contract.
func renderNoteSummary(row EventRow, verb string) string {
	return renderNoteSummaryFromPayload(decodePayload(row.Payload), verb)
}

func renderNoteSummaryFromPayload(payload map[string]any, verb string) string {
	title := readString(payload, "title")
	kind := readString(payload, "kind")
	scope := readString(payload, "scope")
	tags := readStringSlice(payload, "tags")

	parts := []string{}
	if title != "" {
		parts = append(parts, fmt.Sprintf("%q", condenseLine(title)))
	}
	if meta := noteMeta(kind, scope); meta != "" {
		parts = append(parts, "("+meta+")")
	}
	if len(tags) > 0 {
		parts = append(parts, formatNoteTags(tags))
	}

	if len(parts) == 0 {
		return verb
	}
	return verb + " " + strings.Join(parts, " ")
}

// noteMeta composes the "<kind> · <scope>" metadata chunk that lands in
// parens. Either side may be missing (older payloads, partial-write
// regressions); the helper trims silently so the formatter never renders
// a dangling separator.
func noteMeta(kind, scope string) string {
	switch {
	case kind != "" && scope != "":
		return kind + " · " + scope
	case kind != "":
		return kind
	case scope != "":
		return scope
	}
	return ""
}

// formatNoteTags renders the tag-chip suffix ("#alpha #beta"). Empty or
// whitespace-only tags drop out — the storage emit path stores
// already-normalised tag names but the formatter does not assume it.
func formatNoteTags(tags []string) string {
	var b strings.Builder
	first := true
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !first {
			b.WriteByte(' ')
		}
		b.WriteByte('#')
		b.WriteString(t)
		first = false
	}
	return b.String()
}

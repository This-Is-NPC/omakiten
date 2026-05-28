package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SummarizeEvent renders a single-line human-readable detail string
// for a Logs inspector row. The output is the right-most column the
// Logs view shows (5-column generic layout: time · type · entity ·
// who · detail) and the value the CLI / MCP shapes serialise under a
// `summary` field.
//
// Constraints:
//   - Pure. No clock, no I/O, no goroutine state — feed it any
//     EventRow and you get the same string back.
//   - Never panics. Malformed Payload JSON falls back to the raw
//     payload string (condensed); a row with an unknown event_type
//     falls back to "<event_type> <payload-condensed>".
//   - Never empty. Every branch returns at least the event_type as a
//     last resort so the Logs grid never shows a blank cell.
//
// The switch has one arm per KnownEventTypes entry; the parity test
// in event_summary_test.go fails when a new event_type lands in
// event.go without a corresponding arm here.
func SummarizeEvent(row EventRow) string {
	switch row.EventType {
	// ---------- Comments ----------
	case EventTypeComment:
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
	case EventTypeCommentEdited:
		payload := decodePayload(row.Payload)
		from, to := readBodyDelta(payload)
		if from != "" || to != "" {
			return fmt.Sprintf("edited: %q → %q", condenseLine(from), condenseLine(to))
		}
		if id, ok := readInt(payload, "comment_id"); ok {
			return fmt.Sprintf("edited comment #%d", id)
		}
		return "comment edited"
	case EventTypeCommentRemoved:
		payload := decodePayload(row.Payload)
		body := readString(payload, "body")
		if body != "" {
			return fmt.Sprintf("removed: %q", condenseLine(body))
		}
		if id, ok := readInt(payload, "comment_id"); ok {
			return fmt.Sprintf("removed comment #%d", id)
		}
		return "comment removed"

	// ---------- Task lifecycle ----------
	case EventTypeTaskCreated:
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
	case EventTypeTaskMoved:
		payload := decodePayload(row.Payload)
		from := readString(payload, "from")
		to := readString(payload, "to")
		if from != "" || to != "" {
			return fmt.Sprintf("moved %s → %s", fallback(from, "?"), fallback(to, "?"))
		}
		return "task moved"
	case EventTypeTaskMigrated:
		payload := decodePayload(row.Payload)
		from := readString(payload, "from")
		to := readString(payload, "to")
		reason := readString(payload, "reason")
		base := fmt.Sprintf("migrated %s → %s", fallback(from, "?"), fallback(to, "?"))
		if reason != "" {
			base += " (" + reason + ")"
		}
		return base
	case EventTypeTaskBucketOrphaned:
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
	case EventTypeTaskCompleted:
		payload := decodePayload(row.Payload)
		if bucket := readString(payload, "bucket"); bucket != "" {
			return "completed → " + bucket
		}
		return "task completed"
	case EventTypeTaskEdited:
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
	case EventTypeTaskRemoved:
		payload := decodePayload(row.Payload)
		title := readString(payload, "title")
		if title != "" {
			return fmt.Sprintf("removed %q", condenseLine(title))
		}
		return "task removed"
	case EventTypeTaskArchived:
		payload := decodePayload(row.Payload)
		if bucket := readString(payload, "bucket"); bucket != "" {
			return "archived from " + bucket
		}
		return "task archived"
	case EventTypeTaskUnarchived:
		payload := decodePayload(row.Payload)
		if bucket := readString(payload, "bucket"); bucket != "" {
			return "unarchived → " + bucket
		}
		return "task unarchived"
	case EventTypeTaskAssigned:
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
	case EventTypeTaskUnassigned:
		payload := decodePayload(row.Payload)
		if former := readString(payload, "former_assignee"); former != "" {
			return "unassigned " + former
		}
		return "task unassigned"

	// ---------- Project ----------
	case EventTypeProjectRemoved:
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

	// ---------- Plan / wave ----------
	case EventTypePlanCreated:
		payload := decodePayload(row.Payload)
		slug := readString(payload, "slug")
		name := readString(payload, "name")
		switch {
		case slug != "" && name != "":
			return fmt.Sprintf("plan %s (%s)", slug, condenseLine(name))
		case slug != "":
			return "plan " + slug
		case name != "":
			return "plan " + condenseLine(name)
		}
		return "plan created"
	case EventTypePlanWaveAdded:
		payload := decodePayload(row.Payload)
		name := readString(payload, "name")
		if pos, ok := readInt(payload, "position"); ok {
			if name != "" {
				return fmt.Sprintf("wave #%d added: %s", pos, condenseLine(name))
			}
			return fmt.Sprintf("wave #%d added", pos)
		}
		if name != "" {
			return "wave added: " + condenseLine(name)
		}
		return "wave added"
	case EventTypePlanGoalEdited:
		payload := decodePayload(row.Payload)
		if length, ok := readInt(payload, "length"); ok {
			return fmt.Sprintf("goal edited (%d chars)", length)
		}
		return "plan goal edited"
	case EventTypePlanDone:
		return "plan done"
	case EventTypePlanAbandoned:
		payload := decodePayload(row.Payload)
		if reason := readString(payload, "reason"); reason != "" {
			return "plan abandoned: " + condenseLine(reason)
		}
		return "plan abandoned"

	// ---------- Tags / dependencies ----------
	case EventTypeTagAdded:
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
	case EventTypeTagRemoved:
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
	case EventTypeDependencyAdded:
		payload := decodePayload(row.Payload)
		if id, ok := readInt(payload, "depends_on_task_id"); ok {
			return fmt.Sprintf("depends on #%d", id)
		}
		return "dependency added"
	case EventTypeDependencyRemoved:
		payload := decodePayload(row.Payload)
		if id, ok := readInt(payload, "depends_on_task_id"); ok {
			return fmt.Sprintf("dropped dep on #%d", id)
		}
		return "dependency removed"

	// ---------- Guard ----------
	case EventTypeGuardViolated:
		payload := decodePayload(row.Payload)
		op := readString(payload, "operation")
		rule := readString(payload, "rule")
		hint := readString(payload, "hint")
		switch {
		case op != "" && rule != "":
			base := fmt.Sprintf("guard %s/%s", op, rule)
			if hint != "" {
				base += ": " + condenseLine(hint)
			}
			return base
		case rule != "":
			return "guard " + rule
		case op != "":
			return "guard on " + op
		}
		return "guard violated"

	// ---------- Tool calls ----------
	case EventTypeCLIToolCall, EventTypeMCPToolCall, EventTypeTUIToolCall:
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

	// ---------- Hooks ----------
	case EventTypeHookExecuted:
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

	// ---------- Subtask kit notice ----------
	case EventTypeSubtaskKitNoticeEmitted:
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

	// ---------- Bundle ----------
	case EventTypeBundleSwapped:
		payload := decodePayload(row.Payload)
		from := readString(payload, "from_workflow")
		to := readString(payload, "to_workflow")
		orphans, _ := readInt(payload, "orphan_count")
		base := fmt.Sprintf("bundle %s → %s", fallback(from, "?"), fallback(to, "?"))
		if orphans > 0 {
			base += fmt.Sprintf(" (%d orphan(s))", orphans)
		}
		return base
	case EventTypeBundleImported:
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

	// ---------- Confirmation ----------
	case EventTypeConfirmationGranted:
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

	// ---------- Errors / solutions ----------
	case EventTypeErrorRecorded:
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
	case EventTypeErrorSearched:
		payload := decodePayload(row.Payload)
		q := readString(payload, "query")
		count, hasCount := readInt(payload, "result_count")
		switch {
		case q != "" && hasCount:
			return fmt.Sprintf("searched %q → %d hit(s)", condenseLine(q), count)
		case q != "":
			return fmt.Sprintf("searched %q", condenseLine(q))
		case hasCount:
			return fmt.Sprintf("search → %d hit(s)", count)
		}
		return "error searched"
	case EventTypeSolutionAdded:
		payload := decodePayload(row.Payload)
		if id, ok := readInt(payload, "error_id"); ok {
			return fmt.Sprintf("solution added for error #%d", id)
		}
		return "solution added"
	case EventTypeSolutionConfirmed:
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
	case EventTypeSolutionLiked:
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
	case EventTypeSolutionFailed:
		payload := decodePayload(row.Payload)
		if id, ok := readInt(payload, "error_id"); ok {
			return fmt.Sprintf("solution failed (error #%d)", id)
		}
		return "solution failed"
	case EventTypeSolutionViewedTop:
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

	// ---------- Trick palette ----------
	case EventTypeTrickExecuted:
		payload := decodePayload(row.Payload)
		verb := readString(payload, "verb")
		operand := readString(payload, "operand")
		raw := readString(payload, "raw")
		switch {
		case verb != "" && operand != "":
			return fmt.Sprintf("trick %s:%s", verb, condenseLine(operand))
		case verb != "":
			return "trick " + verb
		case raw != "":
			return "trick " + condenseLine(raw)
		}
		return "trick executed"
	}

	// ---------- Unknown ----------
	return unknownFallback(row)
}

// unknownFallback renders rows whose event_type is not in
// KnownEventTypes. Per AC#3 the output is `event_type + " " +
// payload-condensed` so the row is still useful in the Logs grid.
func unknownFallback(row EventRow) string {
	et := strings.TrimSpace(row.EventType)
	if et == "" {
		et = "event"
	}
	payload := strings.TrimSpace(row.Payload)
	if payload == "" {
		return et
	}
	// Re-encode without surrounding whitespace if it parses as JSON;
	// otherwise just collapse runs of whitespace. Either way the
	// result fits on one line.
	if cond, ok := condenseJSON(payload); ok {
		return et + " " + cond
	}
	return et + " " + condenseLine(payload)
}

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

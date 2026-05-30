package domain

import (
	"fmt"
	"strings"
)

func init() {
	registerFormatter(EventTypePlanCreated, summarizePlanCreated)
	registerFormatter(EventTypePlanWaveAdded, summarizePlanWaveAdded)
	registerFormatter(EventTypePlanGoalEdited, summarizePlanGoalEdited)
	registerFormatter(EventTypePlanEdited, summarizePlanEdited)
	registerFormatter(EventTypePlanDeleted, summarizePlanDeleted)
	registerFormatter(EventTypePlanWaveRemoved, summarizePlanWaveRemoved)
	registerFormatter(EventTypePlanWaveRenamed, summarizePlanWaveRenamed)
	registerFormatter(EventTypePlanWaveReordered, summarizePlanWaveReordered)
	registerFormatter(EventTypePlanTaskUnassigned, summarizePlanTaskUnassigned)
	registerFormatter(EventTypePlanDone, summarizePlanDone)
	registerFormatter(EventTypePlanAbandoned, summarizePlanAbandoned)
	registerFormatter(EventTypeTrickExecuted, summarizeTrickExecuted)
}

func summarizePlanCreated(row EventRow) string {
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
}

func summarizePlanWaveAdded(row EventRow) string {
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
}

func summarizePlanGoalEdited(row EventRow) string {
	payload := decodePayload(row.Payload)
	if length, ok := readInt(payload, "length"); ok {
		return fmt.Sprintf("goal edited (%d chars)", length)
	}
	return "plan goal edited"
}

func summarizePlanEdited(row EventRow) string {
	payload := decodePayload(row.Payload)
	fields := readObject(payload, "fields")
	if len(fields) == 0 {
		return "plan edited"
	}
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	strSliceSort(names)
	return "plan edited: " + strings.Join(names, ", ")
}

func summarizePlanDeleted(row EventRow) string {
	payload := decodePayload(row.Payload)
	slug := readString(payload, "slug")
	name := readString(payload, "name")
	switch {
	case slug != "" && name != "":
		return fmt.Sprintf("plan deleted: %s (%s)", slug, condenseLine(name))
	case slug != "":
		return "plan deleted: " + slug
	case name != "":
		return "plan deleted: " + condenseLine(name)
	}
	return "plan deleted"
}

func summarizePlanWaveRemoved(row EventRow) string {
	payload := decodePayload(row.Payload)
	name := readString(payload, "name")
	if pos, ok := readInt(payload, "position"); ok {
		if name != "" {
			return fmt.Sprintf("wave #%d removed: %s", pos, condenseLine(name))
		}
		return fmt.Sprintf("wave #%d removed", pos)
	}
	if name != "" {
		return "wave removed: " + condenseLine(name)
	}
	return "wave removed"
}

func summarizePlanWaveRenamed(row EventRow) string {
	payload := decodePayload(row.Payload)
	from := readString(payload, "from")
	to := readString(payload, "to")
	if from != "" && to != "" {
		return fmt.Sprintf("wave renamed: %s → %s", condenseLine(from), condenseLine(to))
	}
	if to != "" {
		return "wave renamed: " + condenseLine(to)
	}
	return "wave renamed"
}

func summarizePlanWaveReordered(row EventRow) string {
	payload := decodePayload(row.Payload)
	from, fromOK := readInt(payload, "from")
	to, toOK := readInt(payload, "to")
	if fromOK && toOK {
		return fmt.Sprintf("wave reordered: #%d → #%d", from, to)
	}
	if toOK {
		return fmt.Sprintf("wave reordered to #%d", to)
	}
	return "wave reordered"
}

func summarizePlanTaskUnassigned(_ EventRow) string {
	return "task detached from plan"
}

func summarizePlanDone(_ EventRow) string {
	return "plan done"
}

func summarizePlanAbandoned(_ EventRow) string {
	// plan.abandoned always co-emits an empty "{}" payload (see
	// UpdatePlan); there is no "reason" source upstream, so the summary
	// is a fixed line rather than a payload read.
	return "plan abandoned"
}

func summarizeTrickExecuted(row EventRow) string {
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

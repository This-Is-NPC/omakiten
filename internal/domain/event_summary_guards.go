package domain

import (
	"fmt"
)

func init() {
	register(EventTypeGuardViolated, summarizeGuardViolated)
	registerFormatter(EventTypeGuardViolated, summarizeGuardViolated)
}

func summarizeGuardViolated(row EventRow) string {
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
}

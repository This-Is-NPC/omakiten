package domain

import (
	"fmt"
)

func init() {
	registerFormatter(EventTypeAgentSessionStarted, summarizeAgentSessionStarted)
	registerFormatter(EventTypeAgentSessionFinished, summarizeAgentSessionFinished)
	registerFormatter(EventTypeTeamClosed, summarizeTeamClosed)
	registerFormatter(EventTypeTeamFinished, summarizeTeamFinished)
}

func summarizeAgentSessionStarted(row EventRow) string {
	payload := decodePayload(row.Payload)
	member, harness := readString(payload, "member"), readString(payload, "harness")
	if member == "" {
		return "agent session started"
	}
	if harness == "" {
		return "agent " + member + " started"
	}
	return fmt.Sprintf("agent %s started on %s", member, harness)
}

func summarizeAgentSessionFinished(row EventRow) string {
	payload := decodePayload(row.Payload)
	member, status := readString(payload, "member"), readString(payload, "status")
	if member == "" || status == "" {
		return "agent session finished"
	}
	if message := readString(payload, "error"); message != "" {
		return fmt.Sprintf("agent %s %s: %s", member, status, condenseLine(message))
	}
	return fmt.Sprintf("agent %s %s", member, status)
}

func summarizeTeamClosed(row EventRow) string {
	payload := decodePayload(row.Payload)
	team, reason := readString(payload, "team_id"), readString(payload, "reason")
	if team == "" {
		return "agent team closed"
	}
	if reason == "" {
		return "agent team " + team + " closed"
	}
	return fmt.Sprintf("agent team %s closed (%s)", team, reason)
}

func summarizeTeamFinished(row EventRow) string {
	payload := decodePayload(row.Payload)
	team := readString(payload, "team_id")
	if team == "" {
		return "agent team finished"
	}
	done, _ := readInt(payload, "done")
	failed, _ := readInt(payload, "failed")
	return fmt.Sprintf("agent team %s finished: %d done, %d failed", team, done, failed)
}

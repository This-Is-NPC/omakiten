package studioprojection

import (
	"context"
	"encoding/json"
	"strings"

	"omakiten/internal/domain"
)

// EventLister is the narrow read port used by orchestration to prepare hook
// history before a Studio frame is rendered.
type EventLister interface {
	ListEvents(ctx context.Context, filter domain.EventFilter) ([]domain.EventRow, error)
}

// LoadHookHistory reads and groups hook.executed events once for a Studio
// binding. It deliberately returns a projection map, not an adapter consumed
// by screen body functions.
func LoadHookHistory(ctx context.Context, events EventLister, projectID int64, limit int) (map[int][]HookExecuted, error) {
	if events == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	rows, err := events.ListEvents(ctx, domain.EventFilter{
		ProjectID:  projectID,
		Categories: []domain.EventCategory{domain.EventCategoryHook},
		Order:      "desc",
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int][]HookExecuted)
	for _, row := range rows {
		if row.EventType != "" && row.EventType != domain.EventTypeHookExecuted {
			continue
		}
		payload, ok := parseHookExecutedPayload(row)
		if !ok {
			continue
		}
		out[payload.HookIndex] = append(out[payload.HookIndex], HookExecuted{
			CreatedAt: row.CreatedAt, Success: payload.Success, DurationMs: payload.DurationMs,
			EventType: payload.EventType, Error: payload.Error, TargetEventID: payload.TargetEventID,
		})
	}
	return out, nil
}

type hookExecutedPayload struct {
	HookIndex     int    `json:"hook_index"`
	Action        string `json:"action"`
	EventType     string `json:"event_type"`
	TargetEventID int64  `json:"target_event_id"`
	Success       bool   `json:"success"`
	DurationMs    int64  `json:"duration_ms"`
	Error         string `json:"error"`
}

func parseHookExecutedPayload(row domain.EventRow) (hookExecutedPayload, bool) {
	raw := strings.TrimSpace(row.Payload)
	if raw == "" {
		raw = strings.TrimSpace(row.Body)
	}
	if raw == "" {
		return hookExecutedPayload{}, false
	}
	var payload hookExecutedPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return hookExecutedPayload{}, false
	}
	return payload, true
}

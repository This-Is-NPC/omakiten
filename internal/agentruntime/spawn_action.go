package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
)

// spawnAction is the spawn hook action: it starts the roster seat
// args.member of the event's team, named by the payload's team_id, or of
// the active team of its task_id. A seat that already has a session is
// left as it is, so a repeated event starts nothing new.
type spawnAction struct {
	svc *operation.Service
}

func (*spawnAction) Name() string { return config.HookActionSpawn }

func (a *spawnAction) Execute(ctx context.Context, ev domain.Event, args map[string]any) error {
	member, _ := args["member"].(string)
	var payload struct {
		TeamID string `json:"team_id"`
		TaskID int64  `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil || (payload.TeamID == "" && payload.TaskID == 0) {
		return fmt.Errorf("spawn: event %s carries no team_id or task_id", ev.EventType)
	}
	_, err := a.svc.SpawnSeat(ctx, contract.SpawnSeatInput{
		ProjectSelector: contract.ProjectSelector{ProjectID: ev.ProjectID},
		TeamID:          payload.TeamID,
		TaskID:          payload.TaskID,
		Member:          member,
	})
	return err
}

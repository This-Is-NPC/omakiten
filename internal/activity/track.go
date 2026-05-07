package activity

import (
	"context"
	"encoding/json"
	"time"

	"omakiten/internal/domain"
)

func Track(ctx context.Context, operation string, project domain.ProjectContext, args any) func(status string, errMsg string) {
	if skipTracking(ctx) {
		// Caller marked this context as no-track (typically the TUI's
		// realtime refresh tick). Skip both Begin and Finish so the
		// activity log stays focused on intentional agent activity.
		return func(string, string) {}
	}
	repo, ok := repositoryFromContext(ctx)
	if !ok {
		return func(string, string) {}
	}

	source, entrypoint, agentModel, agentSessionID, _ := FromContext(ctx)

	argsJSON := ""
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			argsJSON = "<unserializable>"
		} else {
			argsJSON = string(b)
			if len(argsJSON) > 2048 {
				argsJSON = argsJSON[:2048]
			}
		}
	}

	log := domain.ActivityLog{
		Source:         domain.ActivitySource(source),
		Entrypoint:     entrypoint,
		Operation:      operation,
		ProjectID:      project.ID,
		ProjectSlug:    project.Slug,
		ArgumentsJSON:  argsJSON,
		Status:         "running",
		AgentModel:     agentModel,
		AgentSessionID: agentSessionID,
	}

	id, err := repo.BeginActivityLog(ctx, log)
	if err != nil {
		// Logging failure must not break business logic.
		return func(string, string) {}
	}

	start := time.Now()
	return func(status string, errMsg string) {
		_ = repo.FinishActivityLog(ctx, id, status, int(time.Since(start).Milliseconds()), errMsg)
	}
}

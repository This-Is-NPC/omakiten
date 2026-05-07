package activity

import (
	"context"

	"omakiten/internal/domain"
)

type sourceKey struct{}
type entrypointKey struct{}
type agentModelKey struct{}
type agentSessionKey struct{}
type repoKey struct{}

// WithAgent attaches the four-tuple that lets every downstream service
// attribute its writes (operation events, domain events, denormalized
// rows on errors/solutions). source/entrypoint were already carried;
// agentModel and agentSessionID are new — required for benchmarking
// AI agents (which model recorded what, which session searched first).
func WithAgent(ctx context.Context, source, entrypoint, agentModel, agentSessionID string) context.Context {
	ctx = context.WithValue(ctx, sourceKey{}, source)
	ctx = context.WithValue(ctx, entrypointKey{}, entrypoint)
	ctx = context.WithValue(ctx, agentModelKey{}, agentModel)
	return context.WithValue(ctx, agentSessionKey{}, agentSessionID)
}

func FromContext(ctx context.Context) (source string, entrypoint string, agentModel string, agentSessionID string, ok bool) {
	s, sOk := ctx.Value(sourceKey{}).(string)
	e, _ := ctx.Value(entrypointKey{}).(string)
	m, _ := ctx.Value(agentModelKey{}).(string)
	sess, _ := ctx.Value(agentSessionKey{}).(string)
	return s, e, m, sess, sOk && s != ""
}

type ActivityLogRepository interface {
	BeginActivityLog(ctx context.Context, log any) (int64, error)
	FinishActivityLog(ctx context.Context, id int64, status string, durationMs int, errorMessage string) error
	ListActivityLogs(ctx context.Context, filter domain.ActivityLogFilter) ([]domain.ActivityLog, error)
}

func WithRepository(ctx context.Context, repo ActivityLogRepository) context.Context {
	return context.WithValue(ctx, repoKey{}, repo)
}

func repositoryFromContext(ctx context.Context) (ActivityLogRepository, bool) {
	r, ok := ctx.Value(repoKey{}).(ActivityLogRepository)
	return r, ok && r != nil
}

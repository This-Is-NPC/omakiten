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
type noTrackKey struct{}

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

// WithoutTracking marks ctx so that any downstream `activity.Track`
// call returns the no-op finisher and skips writing to the activity
// log. Used for the TUI's per-second realtime refresh tick — those
// `MetricsService.Summary` / `*.List` calls are renderer-driven and
// should not pollute the per-agent metrics or the log viewer (the
// hint at the bottom of the logs panel already advertises the
// "TUI refreshes and direct reads are not shown" contract).
//
// User-explicit refreshes (`r` key) and refreshes that fire after a
// view-change keep their tracking; only the automatic tick uses
// this flag.
func WithoutTracking(ctx context.Context) context.Context {
	return context.WithValue(ctx, noTrackKey{}, true)
}

// skipTracking reports whether ctx was marked by `WithoutTracking`.
func skipTracking(ctx context.Context) bool {
	v, _ := ctx.Value(noTrackKey{}).(bool)
	return v
}

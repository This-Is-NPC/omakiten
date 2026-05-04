package activity

import (
	"context"

	"omakiten/internal/domain"
)

type sourceKey struct{}
type entrypointKey struct{}
type repoKey struct{}

func WithSource(ctx context.Context, source, entrypoint string) context.Context {
	ctx = context.WithValue(ctx, sourceKey{}, source)
	return context.WithValue(ctx, entrypointKey{}, entrypoint)
}

func FromContext(ctx context.Context) (source string, entrypoint string, ok bool) {
	s, sOk := ctx.Value(sourceKey{}).(string)
	e, eOk := ctx.Value(entrypointKey{}).(string)
	return s, e, sOk && eOk && s != ""
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

package app

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type cappedCommentRepository struct {
	CommentRepository
	called bool
}

func (r *cappedCommentRepository) QueryComments(context.Context, domain.CommentFilter) ([]domain.Comment, error) {
	r.called = true
	return nil, nil
}

func TestCommentServiceRejectsFTSAmplificationBeforeTelemetryAndRepository(t *testing.T) {
	t.Parallel()

	repo := &cappedCommentRepository{}
	telemetry := &cappedSearchActivityRepository{}
	ctx := activity.WithRepository(context.Background(), telemetry)
	query := strings.Repeat("term OR ", domain.SearchQueryMaxTokens/2+1) + "term"
	_, err := NewCommentService(repo, nil).Query(ctx, domain.ProjectContext{}, domain.CommentFilter{Search: query})
	if err == nil {
		t.Fatal("oversized comment FTS query was accepted")
	}
	if repo.called || telemetry.beginCalls != 0 {
		t.Fatalf("oversized comment query reached repository=%v telemetry=%d", repo.called, telemetry.beginCalls)
	}
}

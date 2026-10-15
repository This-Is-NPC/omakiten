package operation

import (
	"cmp"
	"context"
	"path"
	"slices"
	"strings"
	"time"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

const (
	defaultWaitTimeout = 10 * time.Minute
	maxWaitTimeout     = time.Hour
	waitPollInterval   = 250 * time.Millisecond
)

// Wait blocks until a comment on the task, newer than the cursor, carries a
// tag matching input.Tag, or until the timeout. It rereads comments only
// after a commit lands.
func (s *Service) Wait(ctx context.Context, input contract.WaitInput) (contract.WaitResponse, error) {
	if err := s.allow("wait"); err != nil {
		return contract.WaitResponse{}, err
	}
	glob := strings.TrimSpace(input.Tag)
	if _, err := path.Match(glob, ""); glob == "" || err != nil {
		return contract.WaitResponse{}, domain.NewError(domain.ErrValidation, "tag must be a glob such as review-*", map[string]any{"tag": input.Tag})
	}
	timeout, err := waitTimeout(input.Timeout)
	if err != nil {
		return contract.WaitResponse{}, err
	}
	detail, err := s.loadTaskDetail(ctx, input.ProjectSelector, input.TaskID)
	if err != nil {
		return contract.WaitResponse{}, err
	}
	after := input.AfterID
	if after <= 0 {
		if after, err = s.repo.LatestEventID(ctx); err != nil {
			return contract.WaitResponse{}, err
		}
	}
	resp := contract.WaitResponse{Project: projectSummary(detail.project), TaskID: input.TaskID, Matched: []contract.CommentSummary{}, AfterID: after}
	matched, err := s.awaitComments(ctx, detail.project, input.TaskID, glob, after, timeout)
	if err != nil {
		return contract.WaitResponse{}, err
	}
	if len(matched) == 0 {
		resp.TimedOut = true
		return resp, nil
	}
	resp.Matched = commentSummaries(matched)
	resp.AfterID = matched[len(matched)-1].ID
	return resp, nil
}

// awaitComments rereads the task's comments after every commit until some
// match or timeout passes; a timeout returns none.
func (s *Service) awaitComments(ctx context.Context, project domain.ProjectContext, taskID int64, glob string, after int64, timeout time.Duration) ([]domain.Comment, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(waitPollInterval)
	defer tick.Stop()
	version := int64(-1)
	for {
		current, err := s.repo.DataVersion(ctx)
		if err != nil {
			return nil, err
		}
		if current != version {
			version = current
			matched, err := s.matchingComments(ctx, project, taskID, glob, after)
			if err != nil || len(matched) > 0 {
				return matched, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, nil
		case <-tick.C:
		}
	}
}

func waitTimeout(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultWaitTimeout, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d <= 0 || d > maxWaitTimeout {
		return 0, domain.NewError(domain.ErrValidation, "timeout must be a duration between 1s and 1h, such as 10m", map[string]any{"timeout": raw})
	}
	return d, nil
}

// matchingComments returns the task's comments newer than after carrying a
// tag that matches glob, oldest first.
func (s *Service) matchingComments(ctx context.Context, project domain.ProjectContext, taskID int64, glob string, after int64) ([]domain.Comment, error) {
	comments, err := s.newCommentService().List(ctx, project, taskID)
	if err != nil {
		return nil, err
	}
	var out []domain.Comment
	for _, c := range comments {
		if c.ID <= after {
			continue
		}
		for _, tag := range c.Tags {
			if ok, _ := path.Match(glob, tag.Name); ok {
				out = append(out, c)
				break
			}
		}
	}
	slices.SortFunc(out, func(a, b domain.Comment) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

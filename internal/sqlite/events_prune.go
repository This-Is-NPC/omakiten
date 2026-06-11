package sqlite

import (
	"context"
	"fmt"
	"strings"
)

// PruneEventTypes deletes rows matching eventTypes according to the
// resolved retention policy. maxAgeDays and maxRows of zero disable
// that axis. Errors are returned to the caller; hot-path writers swallow
// them so pruning never breaks business logic.
func (s *Store) PruneEventTypes(ctx context.Context, eventTypes []string, maxAgeDays, maxRows int) error {
	if len(eventTypes) == 0 {
		return nil
	}
	if maxAgeDays <= 0 && maxRows <= 0 {
		return nil
	}

	placeholders := strings.Repeat("?,", len(eventTypes))
	placeholders = placeholders[:len(placeholders)-1]
	inClause := "event_type IN (" + placeholders + ")"
	args := make([]any, len(eventTypes))
	for i, et := range eventTypes {
		args[i] = et
	}

	if maxAgeDays > 0 {
		ageArgs := append(append([]any{}, args...), maxAgeDays)
		if _, err := s.db.ExecContext(ctx, `
DELETE FROM events
WHERE `+inClause+` AND created_at < datetime('now', '-' || ? || ' days')
`, ageArgs...); err != nil {
			return fmt.Errorf("prune events by age: %w", err)
		}
	}
	if maxRows > 0 {
		rowArgs := make([]any, 0, len(args)*2+1)
		rowArgs = append(rowArgs, args...)
		rowArgs = append(rowArgs, args...)
		rowArgs = append(rowArgs, maxRows)
		if _, err := s.db.ExecContext(ctx, `
DELETE FROM events
WHERE `+inClause+` AND id NOT IN (
  SELECT id FROM events WHERE `+inClause+` ORDER BY created_at DESC, id DESC LIMIT ?
)
`, rowArgs...); err != nil {
			return fmt.Errorf("prune events by row cap: %w", err)
		}
	}
	return nil
}

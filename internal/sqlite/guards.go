package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

type transitionGuard struct {
	Type    string   `json:"type"`
	Buckets []string `json:"buckets,omitempty"`
	Count   int      `json:"count,omitempty"`
	Tag     string   `json:"tag,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

func evaluateTransitionGuards(ctx context.Context, tx *sql.Tx, projectID, taskID, fromBucketID, toBucketID int64) error {
	var guardsJSON string
	err := tx.QueryRowContext(ctx, `
SELECT guards_json FROM workflow_transitions
WHERE from_bucket_id = ? AND to_bucket_id = ? AND active = 1
LIMIT 1
`, fromBucketID, toBucketID).Scan(&guardsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	var guards []transitionGuard
	if err := json.Unmarshal([]byte(guardsJSON), &guards); err != nil {
		return err
	}

	for _, guard := range guards {
		switch guard.Type {
		case "blockers_in":
			if err := checkBlockersIn(ctx, tx, projectID, taskID, guard.Buckets, guard.Hint); err != nil {
				return err
			}
		case "comments_min":
			if err := checkCommentsMin(ctx, tx, projectID, taskID, guard.Count, guard.Hint); err != nil {
				return err
			}
		case "comments_tagged":
			if err := checkCommentsTagged(ctx, tx, projectID, taskID, guard.Tag, guard.Count, guard.Hint); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkBlockersIn(ctx context.Context, tx *sql.Tx, projectID, taskID int64, allowedKeys []string, hint string) error {
	rows, err := tx.QueryContext(ctx, `
SELECT t.id, t.title, COALESCE(wb.key, '') AS bucket_key
FROM task_dependencies td
JOIN tasks t ON t.project_id = td.project_id AND t.id = td.depends_on_task_id
LEFT JOIN workflow_buckets wb ON wb.id = t.bucket_id AND wb.active = 1
WHERE td.project_id = ? AND td.task_id = ?
ORDER BY t.id
`, projectID, taskID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, k := range allowedKeys {
		allowed[k] = struct{}{}
	}

	var pending []string
	for rows.Next() {
		var id int64
		var title, bucketKey string
		if err := rows.Scan(&id, &title, &bucketKey); err != nil {
			return err
		}
		if _, ok := allowed[bucketKey]; !ok {
			pending = append(pending, fmt.Sprintf("#%d %q (in %q)", id, title, bucketKey))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pending) > 0 {
		msg := fmt.Sprintf("blockers_in guard: pending blockers: %s", strings.Join(pending, ", "))
		details := map[string]any{"pending_blockers": pending}
		if hint != "" {
			msg += ". Hint: " + hint
			details["hint"] = hint
		}
		return domain.NewError(domain.ErrGuardViolation, msg, details)
	}
	return nil
}

func checkCommentsMin(ctx context.Context, tx *sql.Tx, projectID, taskID int64, minCount int, hint string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(1) FROM events WHERE entity_type = 'task' AND event_type = 'comment' AND project_id = ? AND entity_id = ?
`, projectID, taskID).Scan(&count); err != nil {
		return err
	}
	if count < minCount {
		msg := fmt.Sprintf("comments_min guard: task has %d comment(s); transition requires at least %d", count, minCount)
		details := map[string]any{"count": count, "required": minCount}
		if hint != "" {
			msg += ". Hint: " + hint
			details["hint"] = hint
		}
		return domain.NewError(domain.ErrGuardViolation, msg, details)
	}
	return nil
}

func checkCommentsTagged(ctx context.Context, tx *sql.Tx, projectID, taskID int64, tagName string, minCount int, hint string) error {
	if minCount < 1 {
		minCount = 1
	}
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT e.id)
FROM events e
JOIN event_tags et ON et.event_id = e.id
JOIN tags t ON t.id = et.tag_id
WHERE e.entity_type = 'task' AND e.event_type = 'comment' AND e.project_id = ? AND e.entity_id = ? AND t.name = ?
`, projectID, taskID, tagName).Scan(&count); err != nil {
		return err
	}
	if count < minCount {
		msg := fmt.Sprintf("comments_tagged guard: task has %d comment(s) tagged %q; transition requires at least %d", count, tagName, minCount)
		details := map[string]any{"count": count, "required": minCount, "tag": tagName}
		if hint != "" {
			msg += ". Hint: " + hint
			details["hint"] = hint
		}
		return domain.NewError(domain.ErrGuardViolation, msg, details)
	}
	return nil
}

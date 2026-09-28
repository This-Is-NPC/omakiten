package studioprojection

import (
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/config"
)

// HookExecuted is the persistence-free hook history row used by Studio.
type HookExecuted struct {
	CreatedAt     string
	Success       bool
	DurationMs    int64
	EventType     string
	Error         string
	TargetEventID int64
}

// HookIsNotify classifies a hook by its notification action.
func HookIsNotify(spec config.HookSpec) bool { return strings.TrimSpace(spec.Notification) != "" }

// HookShapeCounts returns notification and exec counts.
func HookShapeCounts(hooks []config.HookSpec) (notify, exec int) {
	for _, hook := range hooks {
		switch {
		case HookIsNotify(hook):
			notify++
		case strings.TrimSpace(hook.Do) == "exec":
			exec++
		}
	}
	return notify, exec
}

// HookWhenShort chooses the most useful compact trigger value.
func HookWhenShort(when map[string]string) string {
	for _, key := range []string{"rule", "operation", "author_type"} {
		if value := strings.TrimSpace(when[key]); value != "" {
			return value
		}
	}
	keys := make([]string, 0, len(when))
	for key := range when {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return strings.TrimSpace(when[keys[0]])
}

// HookWhenFull formats all trigger fields deterministically.
func HookWhenFull(when map[string]string) string {
	if len(when) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(when))
	for key := range when {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+when[key])
	}
	return strings.Join(parts, " ")
}

// HookArgvText formats the configured argv field.
func HookArgvText(args map[string]interface{}) string {
	if args == nil || args["argv"] == nil {
		return "-"
	}
	switch value := args["argv"].(type) {
	case []string:
		if len(value) == 0 {
			return "-"
		}
		return strings.Join(value, " ")
	case []interface{}:
		if len(value) == 0 {
			return "-"
		}
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(value)
	}
}

// HookTimeoutText formats timeout_ms values decoded by YAML as common scalar types.
func HookTimeoutText(args map[string]interface{}) string {
	if args == nil || args["timeout_ms"] == nil {
		return "-"
	}
	switch value := args["timeout_ms"].(type) {
	case int:
		return fmt.Sprintf("%d", value)
	case int64:
		return fmt.Sprintf("%d", value)
	case float64:
		return fmt.Sprintf("%d", int64(value))
	default:
		return fmt.Sprint(value)
	}
}

// HookLastLabel classifies the most recent execution.
func HookLastLabel(history []HookExecuted, timeoutLabel string) string {
	if len(history) == 0 {
		return "-"
	}
	if history[0].Success {
		return "ok"
	}
	err := strings.ToLower(history[0].Error)
	if strings.Contains(err, "timeout") || strings.Contains(err, "deadline") {
		return timeoutLabel
	}
	return "fail"
}

// HookStatus returns the compact status token.
func HookStatus(row HookExecuted, okLabel, failLabel string) string {
	if row.Success {
		return okLabel
	}
	return failLabel
}

// HookEntity returns the target event identity or the stable hook fallback.
func HookEntity(row HookExecuted, index int) string {
	if row.TargetEventID > 0 {
		return fmt.Sprintf("#%d", row.TargetEventID)
	}
	return fmt.Sprintf("#%d", index+1)
}

// HookDetail formats execution duration and failure detail.
func HookDetail(row HookExecuted, duration string) string {
	detail := duration
	if err := strings.TrimSpace(row.Error); err != "" {
		detail += "  " + err
	}
	return detail
}

// HookHistoryTime keeps the rightmost timestamp component at the requested width.
func HookHistoryTime(timestamp string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(timestamp) <= width {
		return timestamp
	}
	return timestamp[len(timestamp)-width:]
}

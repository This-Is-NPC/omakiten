package config

import (
	"fmt"
	"sort"

	"omakiten/internal/domain"
)

// EventRetentionSettings declares optional retention knobs for one layer
// of the events.retention inheritance chain. Nil pointers inherit from
// the layer below; explicit zero means unlimited for that axis.
type EventRetentionSettings struct {
	MaxAgeDays *int `yaml:"max_age_days,omitempty" json:"max_age_days,omitempty"`
	MaxRows    *int `yaml:"max_rows,omitempty" json:"max_rows,omitempty"`
}

// EventRetentionDefaults is the required floor for config.events.retention.
// Both fields must be declared (0 = unlimited).
type EventRetentionDefaults struct {
	MaxAgeDays int `yaml:"max_age_days" json:"max_age_days"`
	MaxRows    int `yaml:"max_rows" json:"max_rows"`
}

// EventsRetentionBlock groups the three-level retention policy for rows
// in the unified events table.
type EventsRetentionBlock struct {
	Defaults   EventRetentionDefaults              `yaml:"defaults" json:"defaults"`
	ByCategory map[string]EventRetentionSettings   `yaml:"by_category,omitempty" json:"by_category,omitempty"`
	Overrides  map[string]EventRetentionSettings   `yaml:"overrides,omitempty" json:"overrides,omitempty"`
}

// RetentionGroup is a precomputed prune scope: every event_type that
// resolves to the same (max_age_days, max_rows) pair.
type RetentionGroup struct {
	MaxAgeDays int
	MaxRows    int
	EventTypes []string
}

// ResolvedRetention is the materialised per-event_type retention policy.
type ResolvedRetention struct {
	MaxAgeDays int
	MaxRows    int
}

// NormalizeEventsRetention merges legacy config.activity_log into
// events.retention.by_category.tool_call when the category policy is
// absent, then ensures retention.defaults is populated from the kit
// canonical when callers left it zeroed (test fixtures).
func NormalizeEventsRetention(cfg *Settings, kit Settings) {
	if cfg.Events.Retention.ByCategory == nil {
		cfg.Events.Retention.ByCategory = map[string]EventRetentionSettings{}
	}
	if cfg.Events.Retention.Overrides == nil {
		cfg.Events.Retention.Overrides = map[string]EventRetentionSettings{}
	}

	if cfg.Events.Retention.Defaults.MaxAgeDays == 0 && cfg.Events.Retention.Defaults.MaxRows == 0 {
		cfg.Events.Retention.Defaults = kit.Events.Retention.Defaults
	}
	for cat, layer := range kit.Events.Retention.ByCategory {
		if _, ok := cfg.Events.Retention.ByCategory[cat]; !ok {
			cfg.Events.Retention.ByCategory[cat] = layer
		}
	}

	toolCall, hasToolCall := cfg.Events.Retention.ByCategory["tool_call"]
	toolCallUnset := !hasToolCall || (toolCall.MaxAgeDays == nil && toolCall.MaxRows == nil)
	if toolCallUnset && cfg.ActivityLog.MaxRows > 0 && cfg.ActivityLog.MaxAgeDays > 0 {
		rows := cfg.ActivityLog.MaxRows
		days := cfg.ActivityLog.MaxAgeDays
		cfg.Events.Retention.ByCategory["tool_call"] = EventRetentionSettings{
			MaxAgeDays: &days,
			MaxRows:    &rows,
		}
	}
}

// ResolveRetention returns the storage retention for eventType using
// overrides → by_category → defaults inheritance.
func (e EventsSettings) ResolveRetention(eventType string) ResolvedRetention {
	merged := retentionFromDefaults(e.Retention.Defaults)
	if cat := e.eventCategory(eventType); cat != "" {
		if layer, ok := e.Retention.ByCategory[cat]; ok {
			merged = mergeRetention(merged, layer)
		}
	}
	if layer, ok := e.Retention.Overrides[eventType]; ok {
		merged = mergeRetention(merged, layer)
	}
	return merged
}

// BuildRetentionGroups folds every known event_type into prune groups.
// Types with unlimited retention (both axes zero) are omitted.
func (e EventsSettings) BuildRetentionGroups() []RetentionGroup {
	seen := map[string]struct{}{}
	types := make([]string, 0, len(e.Definitions))
	for eventType := range e.Definitions {
		types = append(types, eventType)
	}
	sort.Strings(types)

	buckets := map[string]*RetentionGroup{}
	for _, eventType := range types {
		resolved := e.ResolveRetention(eventType)
		if resolved.MaxAgeDays == 0 && resolved.MaxRows == 0 {
			continue
		}
		key := fmt.Sprintf("%d:%d", resolved.MaxAgeDays, resolved.MaxRows)
		grp, ok := buckets[key]
		if !ok {
			grp = &RetentionGroup{
				MaxAgeDays: resolved.MaxAgeDays,
				MaxRows:    resolved.MaxRows,
			}
			buckets[key] = grp
		}
		if _, dup := seen[eventType]; dup {
			continue
		}
		seen[eventType] = struct{}{}
		grp.EventTypes = append(grp.EventTypes, eventType)
	}

	out := make([]RetentionGroup, 0, len(buckets))
	for _, grp := range buckets {
		sort.Strings(grp.EventTypes)
		out = append(out, *grp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MaxAgeDays != out[j].MaxAgeDays {
			return out[i].MaxAgeDays < out[j].MaxAgeDays
		}
		return out[i].MaxRows < out[j].MaxRows
	})
	return out
}

func (e EventsSettings) eventCategory(eventType string) string {
	if def, ok := e.Definitions[eventType]; ok && def.Category != "" {
		return def.Category
	}
	if cat := domain.EventCategoryOf(eventType); cat != domain.EventCategoryUnknown {
		return string(cat)
	}
	return ""
}

func retentionFromDefaults(d EventRetentionDefaults) ResolvedRetention {
	return ResolvedRetention(d)
}

// warnLogsWindowExceedsRetention emits a soft warning when the LOGS
// display window exceeds the resolved tool_call storage retention.
func warnLogsWindowExceedsRetention(cfg Settings) []SourceWarning {
	windowDays := cfg.Views.Logs.WindowDays
	if windowDays <= 0 {
		return nil
	}
	resolved := cfg.Events.ResolveRetention("mcp.tool_call")
	if resolved.MaxAgeDays <= 0 || windowDays <= resolved.MaxAgeDays {
		return nil
	}
	return []SourceWarning{{
		Message: fmt.Sprintf(
			"config.views.logs.window_days (%d) exceeds tool_call retention max_age_days (%d); older tool calls are already pruned from storage",
			windowDays, resolved.MaxAgeDays,
		),
	}}
}

func mergeRetention(base ResolvedRetention, layer EventRetentionSettings) ResolvedRetention {
	if layer.MaxAgeDays != nil {
		base.MaxAgeDays = *layer.MaxAgeDays
	}
	if layer.MaxRows != nil {
		base.MaxRows = *layer.MaxRows
	}
	return base
}

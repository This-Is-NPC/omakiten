package config

import (
	"fmt"

	"omakiten/internal/domain"
)

// BuildEventRegistry validates typed configuration and builds project-owned metadata.
func BuildEventRegistry(events EventsSettings) (*domain.EventRegistry, error) {
	defs := make([]domain.EventDef, 0, len(events.Definitions))
	for key, raw := range events.Definitions {
		if raw.Category == "" {
			return nil, fmt.Errorf("event registry: %q missing category", key)
		}
		formatter, ok := domain.ResolveFormatter(domain.FormatterID(raw.Formatter))
		if !ok {
			return nil, fmt.Errorf("event registry: %q unknown formatter %q", key, raw.Formatter)
		}
		d := domain.EventDef{Key: key, Category: domain.EventCategory(raw.Category), Display: raw.Display, Formatter: formatter}
		d.EntityType = events.Defaults.EntityType
		if raw.EntityType != nil {
			d.EntityType = *raw.EntityType
		}
		d.Metric = events.Defaults.Metric
		if raw.Metric != nil {
			d.Metric = *raw.Metric
		}
		if events.Defaults.LogVisible != nil {
			d.LogVisible = *events.Defaults.LogVisible
		}
		if raw.LogVisible != nil {
			d.LogVisible = *raw.LogVisible
		}
		defs = append(defs, d)
	}
	return domain.NewEventRegistry(defs), nil
}

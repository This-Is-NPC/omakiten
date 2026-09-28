package domain

import (
	"sort"
)

// EventDef describes an event's category, visibility and formatter.
type EventDef struct {
	Key        string
	Category   EventCategory
	Display    string
	EntityType string
	Metric     string
	LogVisible bool
	Formatter  func(EventRow) string
}

// EventRegistry owns immutable metadata for one project's events.
type EventRegistry struct {
	definitions []EventDef
	byKey       map[string]EventDef
	categories  map[EventCategory][]string
}

func NewEventRegistry(definitions []EventDef) *EventRegistry {
	r := &EventRegistry{definitions: append([]EventDef(nil), definitions...), byKey: map[string]EventDef{}, categories: map[EventCategory][]string{}}
	sort.Slice(r.definitions, func(i, j int) bool { return r.definitions[i].Key < r.definitions[j].Key })
	for _, d := range r.definitions {
		r.byKey[d.Key] = d
		r.categories[d.Category] = append(r.categories[d.Category], d.Key)
	}
	return r
}
func (r *EventRegistry) Definitions() []EventDef {
	if r == nil {
		return nil
	}
	return append([]EventDef(nil), r.definitions...)
}
func (r *EventRegistry) Types() []string {
	if r == nil {
		return nil
	}
	out := make([]string, len(r.definitions))
	for i, d := range r.definitions {
		out[i] = d.Key
	}
	return out
}
func (r *EventRegistry) CategoryOf(key string) EventCategory {
	if r != nil {
		if d, ok := r.byKey[key]; ok {
			return d.Category
		}
	}
	return EventCategoryUnknown
}
func (r *EventRegistry) TypesForCategory(c EventCategory) []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.categories[c]...)
}
func (r *EventRegistry) Summarize(row EventRow) string {
	if r != nil {
		if d, ok := r.byKey[row.EventType]; ok && d.Formatter != nil {
			return d.Formatter(row)
		}
	}
	return unknownFallback(row)
}

// Prepare annotates a read row without modifying the registry or its input.
func (r *EventRegistry) Prepare(row EventRow) EventRow {
	row.LogHidden = false
	row.Display = row.EventType
	row.Category = r.CategoryOf(row.EventType)
	row.Summary = r.Summarize(row)
	if r != nil {
		if d, ok := r.byKey[row.EventType]; ok {
			row.LogHidden = !d.LogVisible
			if d.Display != "" {
				row.Display = d.Display
			}
		}
	}
	return row
}

// Definition returns metadata for a registered event key.
func (r *EventRegistry) Definition(key string) (EventDef, bool) {
	if r == nil {
		return EventDef{}, false
	}
	def, ok := r.byKey[key]
	return def, ok
}

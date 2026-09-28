package domain

import (
	"testing"
)

func TestEventRegistriesKeepProjectMetadataIndependent(t *testing.T) {
	first := NewEventRegistry([]EventDef{{Key: "project.event", Category: EventCategoryTask, LogVisible: true, Formatter: func(EventRow) string { return "first" }}})
	second := NewEventRegistry([]EventDef{{Key: "project.event", Category: EventCategoryAudit, Formatter: func(EventRow) string { return "second" }}})
	row := EventRow{EventType: "project.event"}
	if first.CategoryOf(row.EventType) != EventCategoryTask || first.Summarize(row) != "first" {
		t.Fatal("second registry changed first project metadata")
	}
	if second.CategoryOf(row.EventType) != EventCategoryAudit || second.Summarize(row) != "second" {
		t.Fatal("second registry lost its own metadata")
	}
	defs := first.Definitions()
	defs[0].Category = EventCategoryAudit
	keys := first.TypesForCategory(EventCategoryTask)
	keys[0] = "changed"
	if first.CategoryOf(row.EventType) != EventCategoryTask || first.TypesForCategory(EventCategoryTask)[0] != row.EventType {
		t.Fatal("returned metadata mutates a registry")
	}
}

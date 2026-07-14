package domain

import (
	"reflect"
	"testing"
)

func TestSearchEntityTypesAreTheFivePhysicalTypes(t *testing.T) {
	want := []SearchEntityType{
		SearchEntityTask,
		SearchEntityComment,
		SearchEntityError,
		SearchEntitySolution,
		SearchEntityPlan,
	}
	if got := AllSearchEntityTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllSearchEntityTypes() = %#v, want %#v", got, want)
	}
	for _, entityType := range want {
		if !IsValidSearchEntityType(string(entityType)) {
			t.Errorf("IsValidSearchEntityType(%q) = false", entityType)
		}
	}
	if IsValidSearchEntityType("note") {
		t.Fatal("IsValidSearchEntityType(note) = true")
	}
}

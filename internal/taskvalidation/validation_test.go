package taskvalidation

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateTitleAndParent(t *testing.T) {
	if !errors.Is(ValidateTitle("  "), ErrTitleRequired) {
		t.Fatal("blank title should be required")
	}
	if err := ValidateTitle("Task"); err != nil {
		t.Fatalf("valid title: %v", err)
	}
	if err := ValidateTitle(strings.Repeat("x", 513)); err == nil {
		t.Fatal("overlong title should be rejected")
	}
	if got, err := PositiveID(" 42 "); err != nil || got != 42 {
		t.Fatalf("PositiveID = %d, %v; want 42", got, err)
	}
	if _, err := PositiveID("0"); err == nil {
		t.Fatal("zero parent should be rejected")
	}
}

func TestCanonicalTags(t *testing.T) {
	got := CanonicalTags(" Alpha Tag,alias,alias ", map[string]string{"alias": "canonical"})
	want := []string{"alpha-tag", "canonical"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("CanonicalTags = %#v, want %#v", got, want)
	}
}

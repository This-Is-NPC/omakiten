package app

import (
	"testing"

	"omakiten/internal/config"
)

// kitSynonyms returns the canonical tag-synonym table from the
// embedded kit YAML for this package's tests. Production threads the
// same map per-project via Phase 3f service setters.
func kitSynonyms() map[string]string {
	return config.MustLoadKitConfig().TagSynonyms
}

func TestNormalizeTagName(t *testing.T) {
	synonyms := kitSynonyms()
	cases := []struct {
		input string
		want  string
	}{
		{"go", "go"},
		{"Go", "go"},
		{"GO", "go"},
		{"golang", "go"},    // synonym
		{"Golang", "go"},    // synonym, case-insensitive
		{"GOLANG", "go"},    // synonym, all caps
		{"javascript", "js"}, // synonym
		{"postgres", "postgresql"}, // synonym
		{"node-js", "node"}, // synonym
		{"node js", "node"}, // synonym via normalization + synonym map
		{"node_js", "node"}, // underscore
		{"postgresql", "postgresql"}, // already canonical
		{"my-tag", "my-tag"},
		{"My Tag", "my-tag"},
		{"  spaces  ", "spaces"},
		{"hello--world", "hello-world"}, // collapse hyphens
		{"-leading", "leading"},
		{"trailing-", "trailing"},
		{"sp3c!@#$%l", "sp3cl"},         // strip special chars
		{"", ""},
		{"---", ""},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeTagName(tc.input, synonyms)
			if got != tc.want {
				t.Errorf("NormalizeTagName(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNormalizeTagNamePerProjectSynonyms locks the Phase 3f isolation:
// two callers passing different synonym tables get different canonical
// names for the same raw input.
func TestNormalizeTagNamePerProjectSynonyms(t *testing.T) {
	projectA := map[string]string{"go": "golang"}
	projectB := map[string]string{"go": "goroutine"}

	if got := NormalizeTagName("go", projectA); got != "golang" {
		t.Errorf("project A: NormalizeTagName(go) = %q, want golang", got)
	}
	if got := NormalizeTagName("go", projectB); got != "goroutine" {
		t.Errorf("project B: NormalizeTagName(go) = %q, want goroutine", got)
	}
	if got := NormalizeTagName("go", nil); got != "go" {
		t.Errorf("nil synonyms: NormalizeTagName(go) = %q, want go (no substitution)", got)
	}
}

// TestTagServicePerProjectSynonymsIsolation drives the per-service
// SetSynonyms path that production composition roots wire: two
// TagService instances running side-by-side normalise the same raw
// tag name to different canonical names because each carries its
// project's synonym table.
func TestTagServicePerProjectSynonymsIsolation(t *testing.T) {
	svcA := NewTagService(nil)
	svcA.SetSynonyms(map[string]string{"go": "golang"})
	svcB := NewTagService(nil)
	svcB.SetSynonyms(map[string]string{"go": "goroutine"})

	if got := NormalizeTagName("go", svcA.synonyms); got != "golang" {
		t.Fatalf("svcA: %q want golang", got)
	}
	if got := NormalizeTagName("go", svcB.synonyms); got != "goroutine" {
		t.Fatalf("svcB: %q want goroutine", got)
	}
}

func TestTagLabel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"go", "Go"},
		{"Go", "Go"},
		{"GO", "GO"},
		{"postgresql", "Postgresql"},
		{"  trimmed  ", "Trimmed"},
		{"", ""},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := TagLabel(tc.input)
			if got != tc.want {
				t.Errorf("TagLabel(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

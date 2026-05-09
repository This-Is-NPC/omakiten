package app

import (
	"testing"

	"omakiten/internal/config"
)

// init loads the canonical tag-synonym table from the embedded kit YAML
// so NormalizeTagName resolves "golang" → "go" etc. in this package's
// tests. Production wires the same map from the user's bundle via
// agentruntime.Open / cli.runtimeOptions.open.
func init() {
	kit := config.MustLoadKitConfig()
	RegisterTagSynonyms(kit.TagSynonyms)
}

func TestNormalizeTagName(t *testing.T) {
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
			got := NormalizeTagName(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeTagName(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
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

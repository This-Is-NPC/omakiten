package agent

import (
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// init seeds the stopwords registry from the embedded kit so similar-task
// scoring filters tokens like "the" / "and" in this package's tests.
// Production wires the same list from the user's bundle via
// agentruntime.Open / cli.runtimeOptions.open.
func init() {
	kit := config.MustLoadKitConfig()
	RegisterStopWords(kit.Search.Stopwords)
}

func TestContextSnippets(t *testing.T) {
	entries := []domain.ContextEntry{
		{ID: 1, Body: "a"},
		{ID: 2, Body: "b"},
		{ID: 3, Body: "c"},
	}
	if len(contextSnippets(entries, 0)) != 3 {
		t.Fatalf("contextSnippets(limit 0) len = %d, want 3", len(contextSnippets(entries, 0)))
	}
	if len(contextSnippets(entries, 2)) != 2 {
		t.Fatalf("contextSnippets(limit 2) len = %d, want 2", len(contextSnippets(entries, 2)))
	}
}

func TestRecentComments(t *testing.T) {
	comments := []domain.Comment{
		{ID: 1, Body: "first"},
		{ID: 2, Body: "second"},
		{ID: 3, Body: "third"},
	}
	result := recentComments(comments, 2)
	if len(result) != 2 {
		t.Fatalf("recentComments(limit 2) len = %d, want 2", len(result))
	}
	if result[0].Body != "third" {
		t.Fatalf("recentComments()[0].Body = %q, want third", result[0].Body)
	}
	if result[1].Body != "second" {
		t.Fatalf("recentComments()[1].Body = %q, want second", result[1].Body)
	}
}

func TestTaskTitleAndDescription(t *testing.T) {
	tests := []struct {
		title       string
		description string
		wantTitle   string
		wantDesc    string
	}{
		{"", "", "", ""},
		{"Title", "Desc", "Title", "Desc"},
		{"", "First line\nSecond", "First line", "First line\nSecond"},
		{"", "Very long first line that exceeds ninety characters which is the limit we set", "Very long first line that exceeds ninety characters which is the limit we set", "Very long first line that exceeds ninety characters which is the limit we set"},
	}
	for _, tc := range tests {
		gotTitle, gotDesc := taskTitleAndDescription(tc.title, tc.description)
		if gotTitle != tc.wantTitle || gotDesc != tc.wantDesc {
			t.Errorf("taskTitleAndDescription(%q, %q) = (%q, %q), want (%q, %q)", tc.title, tc.description, gotTitle, gotDesc, tc.wantTitle, tc.wantDesc)
		}
	}
}

func TestSimilarTasks(t *testing.T) {
	tasks := []domain.Task{
		{ID: 1, Title: "Add MCP agent integration", Description: "Expose Omakiten state"},
		{ID: 2, Title: "Write tests", Description: "Cover code"},
		{ID: 3, Title: "Build API", Description: "REST endpoints"},
	}

	// Empty query -> no matches
	if len(similarTasks("", tasks, 5)) != 0 {
		t.Fatal("similarTasks(empty) should return no matches")
	}

	// Exact match
	exact := similarTasks("Add MCP agent integration", tasks, 5)
	if len(exact) != 1 || exact[0].ID != 1 {
		t.Fatalf("similarTasks(exact) = %#v, want task 1", exact)
	}

	// Substring match
	sub := similarTasks("MCP", tasks, 5)
	if len(sub) != 1 || sub[0].ID != 1 {
		t.Fatalf("similarTasks(sub) = %#v, want task 1", sub)
	}

	// No matches below threshold
	if len(similarTasks("totally unrelated search query for testing", tasks, 5)) != 0 {
		t.Fatal("similarTasks(no match) should return empty")
	}

	// Limit trimming - use a query that matches nothing perfectly to exercise score path
	limited := similarTasks("write tests cover code", tasks, 1)
	if len(limited) != 1 {
		t.Fatalf("similarTasks(limit) len = %d, want 1", len(limited))
	}
}

func TestWordSet(t *testing.T) {
	set := wordSet("Hello world! The quick Brown Fox123")
	if _, ok := set["hello"]; !ok {
		t.Error("wordSet missing 'hello'")
	}
	if _, ok := set["world"]; !ok {
		t.Error("wordSet missing 'world'")
	}
	if _, ok := set["the"]; ok {
		t.Error("wordSet should filter stop word 'the'")
	}
	if _, ok := set["fox123"]; !ok {
		t.Error("wordSet missing 'fox123'")
	}
	if len(set) != 5 {
		t.Fatalf("wordSet len = %d, want 5", len(set))
	}
}

func TestOverlapScore(t *testing.T) {
	if overlapScore(nil, map[string]struct{}{"a": {}}) != 0 {
		t.Error("overlapScore(empty a) should be 0")
	}
	if overlapScore(map[string]struct{}{"a": {}}, nil) != 0 {
		t.Error("overlapScore(empty b) should be 0")
	}
	a := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	b := map[string]struct{}{"b": {}, "c": {}, "d": {}}
	score := overlapScore(a, b)
	if score != 2.0/3.0 {
		t.Fatalf("overlapScore = %f, want %f", score, 2.0/3.0)
	}
}

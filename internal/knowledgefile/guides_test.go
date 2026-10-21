package knowledgefile

import (
	"testing"
	"testing/fstest"
)

func TestReadGuidesTakesTitleQuestionAndBody(t *testing.T) {
	fsys := fstest.MapFS{
		"README.md":        {Data: []byte("# Index\n")},
		"how-to-b.md":      {Data: []byte("---\nrelates_to: [cli:okt]\n---\n# How to B\n\n**The question:** how does B\nwork?\n\nBody.\n")},
		"a-reference.md":   {Data: []byte("# A reference\n\nNo question here.\n")},
		"internal/skip.md": {Data: []byte("# Contributor page\n")},
		"notes.txt":        {Data: []byte("not a guide")},
	}
	guides, err := ReadGuides(fsys)
	if err != nil {
		t.Fatalf("ReadGuides() error = %v", err)
	}
	if len(guides) != 2 || guides[0].Topic != "a-reference" || guides[1].Topic != "how-to-b" {
		t.Fatalf("guides = %+v, want a-reference then how-to-b", guides)
	}
	b := guides[1]
	if b.Title != "How to B" || b.Question != "how does B work?" {
		t.Fatalf("how-to-b = %+v", b)
	}
	if b.Markdown != "# How to B\n\n**The question:** how does B\nwork?\n\nBody." {
		t.Fatalf("how-to-b markdown = %q, want the body without frontmatter", b.Markdown)
	}
	if guides[0].Question != "" {
		t.Fatalf("a-reference question = %q, want none", guides[0].Question)
	}
}

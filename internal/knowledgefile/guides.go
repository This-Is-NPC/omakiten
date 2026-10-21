package knowledgefile

import (
	"io/fs"
	"path"
	"strings"
)

// guideIndex is the page that indexes the guides; it is not a topic.
const guideIndex = "README.md"

const questionMarker = "**The question:**"

// Guide is one user guide shipped with okt. Topic is its file name without
// the extension.
type Guide struct {
	Topic    string `json:"topic"`
	Title    string `json:"title"`
	Question string `json:"question,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

// ReadGuides returns every guide at the top of fsys, sorted by topic, with
// its title, the question it answers and its Markdown body.
func ReadGuides(fsys fs.FS) ([]Guide, error) {
	names, err := fs.Glob(fsys, "*.md")
	if err != nil {
		return nil, err
	}
	guides := make([]Guide, 0, len(names))
	for _, name := range names {
		if name == guideIndex {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		_, body, err := parseMarkdown(data)
		if err != nil {
			return nil, err
		}
		guides = append(guides, Guide{
			Topic:    strings.TrimSuffix(name, path.Ext(name)),
			Title:    markdownTitle(body, name),
			Question: guideQuestion(body),
			Markdown: body,
		})
	}
	return guides, nil
}

// guideQuestion joins the paragraph that opens with questionMarker.
func guideQuestion(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if len(lines) == 0 {
			if strings.HasPrefix(line, questionMarker) {
				lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, questionMarker)))
			}
			continue
		}
		if line == "" {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

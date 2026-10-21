package arch

import (
	"regexp"
	"strings"
	"testing"

	"omakiten/defaults"
	"omakiten/docs"
	"omakiten/internal/knowledgefile"
)

var backticked = regexp.MustCompile("`([^`]+)`")

// TestSkillTopicsAreShippedGuides keeps every topic the agent skill routes to
// readable through `okt docs show`.
func TestSkillTopicsAreShippedGuides(t *testing.T) {
	skill, err := defaults.FS.ReadFile("agent/omakiten/SKILL.md")
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	guides, err := knowledgefile.ReadGuides(docs.FS)
	if err != nil {
		t.Fatalf("read guides: %v", err)
	}
	shipped := map[string]bool{}
	for _, guide := range guides {
		shipped[guide.Topic] = true
	}

	var topics []string
	inTable := false
	for _, line := range strings.Split(string(skill), "\n") {
		switch {
		case strings.HasPrefix(line, "| Subject | Topic |"):
			inTable = true
		case inTable && !strings.HasPrefix(line, "|"):
			inTable = false
		case inTable && !strings.HasPrefix(line, "| ---"):
			cells := strings.Split(strings.Trim(line, "|"), "|")
			for _, match := range backticked.FindAllStringSubmatch(cells[len(cells)-1], -1) {
				topics = append(topics, match[1])
			}
		}
	}
	if len(topics) == 0 {
		t.Fatal("skill names no guide topics")
	}
	for _, topic := range topics {
		if !shipped[topic] {
			t.Errorf("skill routes to %q, which is not a shipped guide", topic)
		}
	}
}

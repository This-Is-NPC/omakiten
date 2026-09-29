package graph

import (
	"sort"

	"omakiten/internal/domain"
)

// KnowledgeLine is a selectable node or directed edge in the knowledge graph.
type KnowledgeLine struct {
	Text       string
	ResourceID string
}

// KnowledgeLines projects file-backed resources and their outgoing relations.
func KnowledgeLines(snapshot domain.KnowledgeSnapshot) []KnowledgeLine {
	resources := append([]domain.KnowledgeResource(nil), snapshot.Resources...)
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Project+":"+resources[i].ID < resources[j].Project+":"+resources[j].ID
	})
	titles := make(map[string]string, len(resources))
	for _, item := range resources {
		titles[item.Project+":"+item.ID] = item.Title
	}
	links := make(map[string][]domain.KnowledgeRelation)
	for _, relation := range snapshot.Relations {
		links[relation.From] = append(links[relation.From], relation)
	}
	lines := make([]KnowledgeLine, 0, len(resources)+len(snapshot.Relations))
	for _, item := range resources {
		id := item.Project + ":" + item.ID
		lines = append(lines, KnowledgeLine{Text: "● " + id + "  " + item.Title, ResourceID: id})
		outgoing := links[id]
		sort.Slice(outgoing, func(i, j int) bool {
			return outgoing[i].To+":"+outgoing[i].Kind < outgoing[j].To+":"+outgoing[j].Kind
		})
		for i, relation := range outgoing {
			branch := "├─ "
			if i == len(outgoing)-1 {
				branch = "└─ "
			}
			line := KnowledgeLine{Text: "  " + branch + relation.Kind + " → " + relation.To}
			if title, ok := titles[relation.To]; ok {
				line.Text += "  " + title
				line.ResourceID = relation.To
			}
			lines = append(lines, line)
		}
	}
	return lines
}

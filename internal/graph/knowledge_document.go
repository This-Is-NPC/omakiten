package graph

import (
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// KnowledgeDocument assembles a resource and its directly attached documentation.
func KnowledgeDocument(snapshot domain.KnowledgeSnapshot, item domain.KnowledgeResource) string {
	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n`%s:%s` · `%s`\n\n", item.Title, item.Project, item.ID, item.Path)
	if item.Description != "" {
		body.WriteString(item.Description + "\n\n")
	}
	docs := attachedDocuments(snapshot, item.Project+":"+item.ID)
	for _, doc := range docs {
		fmt.Fprintf(&body, "## Documentation: %s\n\n`%s`\n\n%s\n\n", doc.Title, doc.Path, doc.Body)
	}
	if item.Body != "" {
		if len(docs) > 0 {
			body.WriteString("## Reference\n\n")
		}
		body.WriteString(item.Body)
	}
	return body.String()
}

func attachedDocuments(snapshot domain.KnowledgeSnapshot, id string) []domain.KnowledgeResource {
	resources := make(map[string]domain.KnowledgeResource, len(snapshot.Resources))
	for _, item := range snapshot.Resources {
		resources[item.Project+":"+item.ID] = item
	}
	var docs []domain.KnowledgeResource
	seen := make(map[string]bool)
	for _, relation := range snapshot.Relations {
		if relation.From != id || relation.Kind != "documented_by" || seen[relation.To] {
			continue
		}
		if doc, ok := resources[relation.To]; ok {
			docs = append(docs, doc)
			seen[relation.To] = true
		}
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Title != docs[j].Title {
			return docs[i].Title < docs[j].Title
		}
		return docs[i].Project+":"+docs[i].ID < docs[j].Project+":"+docs[j].ID
	})
	return docs
}

package graph

import (
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// KnowledgeLine is one visible node or relation in the project graph.
type KnowledgeLine struct {
	Text       string
	ResourceID string
}

// KnowledgeGraph is the complete, scrollable project knowledge graph.
type KnowledgeGraph struct {
	Lines []KnowledgeLine
}

type knowledgeIndex struct {
	resources map[string]domain.KnowledgeResource
	children  map[string][]domain.KnowledgeRelation
	contained map[string]bool
	visited   map[string]bool
	graph     KnowledgeGraph
}

// ProjectKnowledge places interfaces first and follows their relations in one tree.
func ProjectKnowledge(snapshot domain.KnowledgeSnapshot) KnowledgeGraph {
	index := newKnowledgeIndex(snapshot)
	cli, api, docs := index.roots()
	index.addCategory("CLI", cli)
	index.addCategory("API", api)
	var unlinked []string
	for _, id := range docs {
		if !index.visited[id] {
			unlinked = append(unlinked, id)
		}
	}
	index.addCategory("Documentation", unlinked)
	var other []string
	for id := range index.resources {
		if !index.visited[id] {
			other = append(other, id)
		}
	}
	sort.Slice(other, func(i, j int) bool { return index.lessTitle(other[i], other[j]) })
	index.addCategory("Other resources", other)
	return index.graph
}

func newKnowledgeIndex(snapshot domain.KnowledgeSnapshot) *knowledgeIndex {
	index := &knowledgeIndex{
		resources: make(map[string]domain.KnowledgeResource, len(snapshot.Resources)),
		children:  make(map[string][]domain.KnowledgeRelation),
		contained: make(map[string]bool),
		visited:   make(map[string]bool, len(snapshot.Resources)),
	}
	for _, item := range snapshot.Resources {
		index.resources[item.Project+":"+item.ID] = item
	}
	for _, relation := range snapshot.Relations {
		if _, ok := index.resources[relation.From]; !ok {
			continue
		}
		if _, ok := index.resources[relation.To]; !ok {
			continue
		}
		index.children[relation.From] = append(index.children[relation.From], relation)
		if relation.Kind == "contains" {
			index.contained[relation.To] = true
		}
	}
	for id := range index.children {
		sort.Slice(index.children[id], func(i, j int) bool {
			return index.lessRelation(index.children[id][i], index.children[id][j])
		})
	}
	return index
}

func (index *knowledgeIndex) lessRelation(a, b domain.KnowledgeRelation) bool {
	if relationOrder(a.Kind) != relationOrder(b.Kind) {
		return relationOrder(a.Kind) < relationOrder(b.Kind)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return index.lessTitle(a.To, b.To)
}

func (index *knowledgeIndex) lessTitle(a, b string) bool {
	if index.resources[a].Title != index.resources[b].Title {
		return index.resources[a].Title < index.resources[b].Title
	}
	return a < b
}

func (index *knowledgeIndex) roots() (cli, api, docs []string) {
	for id, item := range index.resources {
		switch {
		case strings.HasPrefix(item.ID, "cli:") && !index.contained[id]:
			cli = append(cli, id)
		case strings.HasPrefix(item.ID, "openapi:") && !strings.HasPrefix(item.ID, "openapi:schema:"):
			api = append(api, id)
		case !strings.HasPrefix(item.ID, "cli:") && !strings.HasPrefix(item.ID, "openapi:"):
			docs = append(docs, id)
		}
	}
	for _, ids := range [][]string{cli, api, docs} {
		sort.Slice(ids, func(i, j int) bool { return index.lessTitle(ids[i], ids[j]) })
	}
	return cli, api, docs
}

func (index *knowledgeIndex) addCategory(title string, roots []string) {
	if len(roots) == 0 {
		return
	}
	index.graph.Lines = append(index.graph.Lines, KnowledgeLine{Text: title})
	for i, id := range roots {
		index.walk(id, "", i == len(roots)-1, "")
	}
}

func (index *knowledgeIndex) walk(id, prefix string, last bool, edge string) {
	branch, continuation := "├─ ", "│  "
	if last {
		branch, continuation = "└─ ", "   "
	}
	label := index.resources[id].Title
	if edge != "" && edge != "contains" {
		label = edge + " → " + label
	}
	if index.visited[id] {
		label += " ↗"
	}
	index.graph.Lines = append(index.graph.Lines, KnowledgeLine{Text: prefix + branch + label, ResourceID: id})
	if index.visited[id] {
		return
	}
	index.visited[id] = true
	outgoing := index.children[id]
	for i, relation := range outgoing {
		index.walk(relation.To, prefix+continuation, i == len(outgoing)-1, relation.Kind)
	}
}

func relationOrder(kind string) int {
	switch kind {
	case "contains":
		return 0
	case "documented_by":
		return 1
	default:
		return 2
	}
}

package graph

import (
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

const KnowledgeRoot = "root"

const (
	knowledgeCLI  = "category:cli"
	knowledgeAPI  = "category:api"
	knowledgeDocs = "category:docs"
)

// KnowledgeLine opens a neighboring item or its document.
type KnowledgeLine struct {
	Text       string
	FocusID    string
	ResourceID string
}

// KnowledgeView is one prepared neighborhood in the project knowledge graph.
type KnowledgeView struct {
	Title  string
	Parent string
	Lines  []KnowledgeLine
}

// KnowledgeMap contains the project entry and every navigable neighborhood.
type KnowledgeMap struct {
	Views map[string]KnowledgeView
}

// KnowledgeViews projects interfaces first and attaches documents to their items.
func KnowledgeViews(snapshot domain.KnowledgeSnapshot) KnowledgeMap {
	resources := make(map[string]domain.KnowledgeResource, len(snapshot.Resources))
	children := make(map[string][]domain.KnowledgeRelation)
	parents := make(map[string]string)
	for _, item := range snapshot.Resources {
		resources[item.Project+":"+item.ID] = item
	}
	for _, relation := range snapshot.Relations {
		children[relation.From] = append(children[relation.From], relation)
		if relation.Kind == "contains" {
			parents[relation.To] = relation.From
		}
	}
	result := KnowledgeMap{Views: make(map[string]KnowledgeView, len(resources)+4)}
	cli, api, docs := knowledgeCategories(resources, parents)
	result.Views[KnowledgeRoot] = knowledgeRootView(cli, api, docs)
	result.Views[knowledgeCLI] = knowledgeCategoryView("CLI", KnowledgeRoot, cli, resources)
	result.Views[knowledgeAPI] = knowledgeCategoryView("API", KnowledgeRoot, api, resources)
	if len(cli) == 0 && len(api) == 0 {
		result.Views[knowledgeDocs] = knowledgeCategoryView("Documentation", KnowledgeRoot, docs, resources)
	}
	for id, item := range resources {
		if !knowledgeInterface(item) {
			continue
		}
		parent := parents[id]
		if parent == "" {
			parent = knowledgeAPI
			if strings.HasPrefix(item.ID, "cli:") {
				parent = knowledgeCLI
			}
		}
		result.Views[id] = knowledgeResourceView(id, item, parent, children[id], resources)
	}
	return result
}

func knowledgeInterface(item domain.KnowledgeResource) bool {
	return strings.HasPrefix(item.ID, "cli:") || strings.HasPrefix(item.ID, "openapi:")
}

func knowledgeCategories(resources map[string]domain.KnowledgeResource, parents map[string]string) (cli, api, docs []string) {
	for id, item := range resources {
		switch {
		case strings.HasPrefix(item.ID, "cli:") && parents[id] == "":
			cli = append(cli, id)
		case strings.HasPrefix(item.ID, "openapi:") && !strings.HasPrefix(item.ID, "openapi:schema:"):
			api = append(api, id)
		case !knowledgeInterface(item):
			docs = append(docs, id)
		}
	}
	sort.Strings(cli)
	sort.Strings(api)
	sort.Strings(docs)
	return cli, api, docs
}

func knowledgeRootView(cli, api, docs []string) KnowledgeView {
	view := KnowledgeView{Title: "PROJECT KNOWLEDGE"}
	if len(cli) > 0 {
		view.Lines = append(view.Lines, KnowledgeLine{Text: "CLI → commands", FocusID: knowledgeCLI})
	}
	if len(api) > 0 {
		view.Lines = append(view.Lines, KnowledgeLine{Text: fmt.Sprintf("API → %d endpoints", len(api)), FocusID: knowledgeAPI})
	}
	if len(docs) > 0 && len(cli) == 0 && len(api) == 0 {
		view.Lines = append(view.Lines, KnowledgeLine{Text: fmt.Sprintf("Documentation → %d pages", len(docs)), FocusID: knowledgeDocs})
	}
	return view
}

func knowledgeCategoryView(title, parent string, ids []string, resources map[string]domain.KnowledgeResource) KnowledgeView {
	view := KnowledgeView{Title: title, Parent: parent}
	for _, id := range ids {
		item := resources[id]
		line := KnowledgeLine{Text: "● " + item.Title, ResourceID: id}
		if knowledgeInterface(item) {
			line.FocusID = id
		}
		view.Lines = append(view.Lines, line)
	}
	return view
}

func knowledgeResourceView(id string, item domain.KnowledgeResource, parent string, outgoing []domain.KnowledgeRelation, resources map[string]domain.KnowledgeResource) KnowledgeView {
	category := "API"
	if strings.HasPrefix(item.ID, "cli:") {
		category = "CLI"
	}
	view := KnowledgeView{Title: category + " › " + item.Title, Parent: parent, Lines: []KnowledgeLine{{Text: "● " + item.Title + "  ·  open details and options", ResourceID: id}}}
	sort.Slice(outgoing, func(i, j int) bool {
		return outgoing[i].Kind+":"+outgoing[i].To < outgoing[j].Kind+":"+outgoing[j].To
	})
	visible := make([]domain.KnowledgeRelation, 0, len(outgoing))
	for _, relation := range outgoing {
		if _, ok := resources[relation.To]; ok {
			visible = append(visible, relation)
		}
	}
	for i, relation := range visible {
		target := resources[relation.To]
		label := relation.Kind
		if label == "contains" {
			label = "command"
		}
		if label == "documented_by" {
			label = "documentation"
		}
		branch := "├─ "
		if i == len(visible)-1 {
			branch = "└─ "
		}
		line := KnowledgeLine{Text: "  " + branch + label + " → " + target.Title, ResourceID: relation.To}
		if knowledgeInterface(target) {
			line.FocusID = relation.To
		}
		view.Lines = append(view.Lines, line)
	}
	return view
}

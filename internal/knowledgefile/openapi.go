package knowledgefile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"omakiten/internal/domain"
)

type apiSpec struct {
	OpenAPI string `yaml:"openapi"`
	Info    struct {
		Title string `yaml:"title"`
	} `yaml:"info"`
	Paths      map[string]map[string]apiOperation `yaml:"paths"`
	Components struct {
		Schemas map[string]apiSchema `yaml:"schemas"`
	} `yaml:"components"`
}

type apiOperation struct {
	OperationID string `yaml:"operationId"`
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
	Parameters  []struct {
		Name        string    `yaml:"name"`
		In          string    `yaml:"in"`
		Description string    `yaml:"description"`
		Schema      apiSchema `yaml:"schema"`
	} `yaml:"parameters"`
	RequestBody struct {
		Content map[string]apiMedia `yaml:"content"`
	} `yaml:"requestBody"`
	Responses map[string]struct {
		Description string              `yaml:"description"`
		Content     map[string]apiMedia `yaml:"content"`
	} `yaml:"responses"`
}

type apiMedia struct {
	Schema apiSchema `yaml:"schema"`
}

type apiSchema struct {
	Ref         string               `yaml:"$ref"`
	Type        any                  `yaml:"type"`
	Description string               `yaml:"description"`
	Properties  map[string]apiSchema `yaml:"properties"`
	Items       *apiSchema           `yaml:"items"`
}

func readOpenAPI(project domain.ProjectContext, path string, result *domain.KnowledgeSnapshot) {
	data, err := readFile(path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("OpenAPI %s: %v", path, err))
		return
	}
	var spec apiSpec
	if err := yaml.Unmarshal(data, &spec); err != nil || !strings.HasPrefix(spec.OpenAPI, "3.") {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("OpenAPI %s: expected a valid OpenAPI 3 document", path))
		return
	}
	count := len(spec.Components.Schemas)
	for _, methods := range spec.Paths {
		for method := range methods {
			if isHTTPMethod(method) {
				count++
			}
		}
	}
	if !hasResourceCapacity(result, count) {
		return
	}
	rel, err := filepath.Rel(project.RootPath, path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, err.Error())
		return
	}
	rel = filepath.ToSlash(rel)
	appendOpenAPIOperations(project.Slug, rel, spec.Paths, result)
	for _, name := range sortedKeys(spec.Components.Schemas) {
		schema := spec.Components.Schemas[name]
		result.Resources = append(result.Resources, domain.KnowledgeResource{ID: "openapi:schema:" + name, Project: project.Slug, Kind: "OpenAPI Schema", Title: name, Description: schema.Description, Body: schema.Description, Path: rel})
	}
}

func appendOpenAPIOperations(project, path string, paths map[string]map[string]apiOperation, result *domain.KnowledgeSnapshot) {
	for _, route := range sortedKeys(paths) {
		methods := sortedKeys(paths[route])
		for _, method := range methods {
			if !isHTTPMethod(method) {
				continue
			}
			appendOpenAPIOperation(project, path, method, route, paths[route][method], result)
		}
	}

}

func appendOpenAPIOperation(project, path, method, route string, op apiOperation, result *domain.KnowledgeSnapshot) {
	id := op.OperationID
	if id == "" {
		id = strings.ToUpper(method) + " " + route
	}
	title := op.Summary
	if title == "" {
		title = strings.ToUpper(method) + " " + route
	}
	result.Resources = append(result.Resources, domain.KnowledgeResource{
		ID: "openapi:" + id, Project: project, Kind: "OpenAPI Operation", Title: title,
		Description: op.Description, Body: operationBody(method, route, op), Path: path,
	})
	from := project + ":openapi:" + id
	for _, p := range op.Parameters {
		linkSchema(project, from, p.Schema, result)
	}
	for _, media := range op.RequestBody.Content {
		linkSchema(project, from, media.Schema, result)
	}
	for _, response := range op.Responses {
		for _, media := range response.Content {
			linkSchema(project, from, media.Schema, result)
		}
	}
}

func operationBody(method, route string, op apiOperation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n\n", strings.ToUpper(method), route)
	if op.Description != "" {
		b.WriteString(op.Description + "\n\n")
	}
	if len(op.Parameters) > 0 {
		b.WriteString("## Parameters\n\n")
		for _, p := range op.Parameters {
			fmt.Fprintf(&b, "- `%s` (%s): %s\n", p.Name, p.In, p.Description)
		}
	}
	if len(op.Responses) > 0 {
		b.WriteString("\n## Responses\n\n")
		for _, code := range sortedKeys(op.Responses) {
			fmt.Fprintf(&b, "- `%s`: %s\n", code, op.Responses[code].Description)
		}
	}
	return b.String()
}

func linkSchema(project, from string, schema apiSchema, result *domain.KnowledgeSnapshot) {
	if strings.HasPrefix(schema.Ref, "#/components/schemas/") {
		result.Relations = append(result.Relations, domain.KnowledgeRelation{From: from, To: project + ":openapi:schema:" + strings.TrimPrefix(schema.Ref, "#/components/schemas/"), Kind: "uses"})
	}
	for _, nested := range schema.Properties {
		linkSchema(project, from, nested, result)
	}
	if schema.Items != nil {
		linkSchema(project, from, *schema.Items, result)
	}
}

func isHTTPMethod(method string) bool {
	switch strings.ToLower(method) {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	}
	return false
}

func sortedKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

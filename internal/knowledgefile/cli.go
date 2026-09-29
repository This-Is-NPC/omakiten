package knowledgefile

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"omakiten/internal/domain"
)

type cliDocument struct {
	Version  int          `yaml:"version"`
	Commands []cliCommand `yaml:"commands"`
}

type cliCommand struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Summary     string   `yaml:"summary"`
	Description string   `yaml:"description"`
	Links       []string `yaml:"links"`
}

func readCLI(project domain.ProjectContext, path string, result *domain.KnowledgeSnapshot) {
	data, err := readFile(path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("CLI source %s: %v", path, err))
		return
	}
	var doc cliDocument
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Version != 1 {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("CLI source %s: expected version 1", path))
		return
	}
	if !hasResourceCapacity(result, len(doc.Commands)) {
		return
	}
	rel, err := filepath.Rel(project.RootPath, path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, err.Error())
		return
	}
	rel = filepath.ToSlash(rel)
	for _, command := range doc.Commands {
		appendCLICommand(project.Slug, rel, command, result)
	}
}

func appendCLICommand(project, path string, command cliCommand, result *domain.KnowledgeSnapshot) {
	if command.ID == "" || command.Name == "" {
		result.Diagnostics = append(result.Diagnostics, path+": CLI commands require id and name")
		return
	}
	id := "cli:" + command.ID
	result.Resources = append(result.Resources, domain.KnowledgeResource{
		ID: id, Project: project, Kind: "CLI Command", Title: command.Name,
		Description: command.Summary, Body: command.Description, Path: path,
	})
	for _, link := range command.Links {
		target, ok := qualifiedKnowledgeLink(project, link)
		if !ok {
			result.Diagnostics = append(result.Diagnostics, path+": invalid CLI command link "+link)
			continue
		}
		result.Relations = append(result.Relations, domain.KnowledgeRelation{From: project + ":" + id, To: target, Kind: "references"})
	}
}

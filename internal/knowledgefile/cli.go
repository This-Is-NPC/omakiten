package knowledgefile

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func readCLI(project domain.ProjectContext, path string, result *domain.KnowledgeSnapshot) {
	data, err := readFile(path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("CLI source %s: %v", path, err))
		return
	}
	var doc contract.CLIInventory
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

func appendCLICommand(project, path string, command contract.CLICommand, result *domain.KnowledgeSnapshot) {
	if command.ID == "" || command.Name == "" {
		result.Diagnostics = append(result.Diagnostics, path+": CLI commands require id and name")
		return
	}
	id := "cli:" + command.ID
	result.Resources = append(result.Resources, domain.KnowledgeResource{
		ID: id, Project: project, Kind: "CLI Command", Title: command.Name,
		Description: command.Summary, Body: cliCommandBody(command), Path: path,
	})
	if command.Parent != "" {
		result.Relations = append(result.Relations, domain.KnowledgeRelation{From: project + ":cli:" + command.Parent, To: project + ":" + id, Kind: "contains"})
	}
	for _, link := range command.Links {
		target, ok := qualifiedKnowledgeLink(project, link)
		if !ok {
			result.Diagnostics = append(result.Diagnostics, path+": invalid CLI command link "+link)
			continue
		}
		result.Relations = append(result.Relations, domain.KnowledgeRelation{From: project + ":" + id, To: target, Kind: "references"})
	}
}

func cliCommandBody(command contract.CLICommand) string {
	var body strings.Builder
	description := command.Description
	if description == "" {
		description = command.Summary
	}
	fmt.Fprintf(&body, "# %s\n\n%s\n\n", command.Name, description)
	if len(command.Flags) > 0 {
		body.WriteString("## Options\n\n")
		for _, flag := range command.Flags {
			fmt.Fprintf(&body, "- `--%s`", flag.Name)
			if flag.Shorthand != "" {
				fmt.Fprintf(&body, " (`-%s`)", flag.Shorthand)
			}
			fmt.Fprintf(&body, ": %s\n", flag.Description)
		}
	}
	return body.String()
}

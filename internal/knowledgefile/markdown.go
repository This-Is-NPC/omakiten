package knowledgefile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

var markdownLink = regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)

type markdownMeta struct {
	Type        string `yaml:"type"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
}

func readMarkdownSource(ctx context.Context, project domain.ProjectContext, format, path string, result *domain.KnowledgeSnapshot) {
	info, err := os.Stat(path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("knowledge source %s: %v", path, err))
		return
	}
	if !info.IsDir() {
		readMarkdown(ctx, project, format, path, filepath.Dir(path), result)
		return
	}
	err = filepath.WalkDir(path, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			result.Diagnostics = append(result.Diagnostics, "knowledge source contains a symlink: "+name)
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		if len(result.Resources) >= maxResources {
			return fmt.Errorf("knowledge resource limit exceeded")
		}
		readMarkdown(ctx, project, format, name, path, result)
		return nil
	})
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, err.Error())
	}
}

func readMarkdown(ctx context.Context, project domain.ProjectContext, format, path, sourceRoot string, result *domain.KnowledgeSnapshot) {
	if ctx.Err() != nil {
		return
	}
	data, err := readFile(path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("knowledge source %s: %v", path, err))
		return
	}
	rel, err := filepath.Rel(project.RootPath, path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, err.Error())
		return
	}
	rel = filepath.ToSlash(rel)
	meta, body, err := parseMarkdown(data)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("%s: %v", rel, err))
		return
	}
	if format == "okf" && meta.Type == "" {
		result.Diagnostics = append(result.Diagnostics, rel+": OKF type is required")
		return
	}
	if meta.Type == "" {
		meta.Type = "Markdown"
	}
	if meta.Title == "" {
		meta.Title = markdownTitle(body, rel)
	}
	id := "markdown:" + strings.TrimSuffix(rel, filepath.Ext(rel))
	result.Resources = append(result.Resources, domain.KnowledgeResource{
		ID: id, Project: project.Slug, Kind: meta.Type, Title: meta.Title,
		Description: meta.Description, Body: body, Path: rel,
	})
	appendMarkdownLinks(project, id, body, path, sourceRoot, result)
}

func parseMarkdown(data []byte) (markdownMeta, string, error) {
	var meta markdownMeta
	body := string(data)
	if !strings.HasPrefix(body, "---\n") && !strings.HasPrefix(body, "---\r\n") {
		return meta, body, nil
	}
	header, content, err := config.SplitFrontmatter(data)
	if err != nil {
		return meta, "", err
	}
	if err := yaml.Unmarshal(header, &meta); err != nil {
		return meta, "", err
	}
	return meta, string(content), nil
}

func appendMarkdownLinks(project domain.ProjectContext, id, body, path, sourceRoot string, result *domain.KnowledgeSnapshot) {
	for _, match := range markdownLink.FindAllStringSubmatch(body, -1) {
		target := strings.SplitN(match[1], "#", 2)[0]
		target = strings.Trim(target, "<>")
		to := markdownTarget(project, target, path, sourceRoot)
		if to != "" {
			result.Relations = append(result.Relations, domain.KnowledgeRelation{From: project.Slug + ":" + id, To: to, Kind: "references"})
		}
	}
}

func markdownTarget(project domain.ProjectContext, target, path, sourceRoot string) string {
	if strings.HasPrefix(target, "okt://") {
		qualified, _ := qualifiedKnowledgeLink(project.Slug, target)
		return qualified
	}
	if !strings.EqualFold(filepath.Ext(target), ".md") || strings.Contains(target, "://") {
		return ""
	}
	absolute := filepath.Join(filepath.Dir(path), target)
	if strings.HasPrefix(target, "/") {
		absolute = filepath.Join(project.RootPath, target[1:])
	}
	within, err := filepath.Rel(sourceRoot, absolute)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return ""
	}
	linked, err := filepath.Rel(project.RootPath, absolute)
	if err != nil {
		return ""
	}
	linked = filepath.ToSlash(linked)
	return project.Slug + ":markdown:" + strings.TrimSuffix(linked, filepath.Ext(linked))
}

func markdownTitle(body, path string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

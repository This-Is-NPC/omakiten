package knowledgefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"omakiten/internal/domain"
)

const maxFileBytes = 1 << 20
const maxResources = 2000

type source struct {
	Format string `yaml:"format"`
	Path   string `yaml:"path"`
}

type manifest struct {
	Version         int      `yaml:"version"`
	Sources         []source `yaml:"sources"`
	RelatedProjects []string `yaml:"related_projects"`
}

// ResolveProject finds a registered project only when cross-project reading is requested.
type ResolveProject func(context.Context, string) (domain.Project, error)

// Load reads the selected project's source files and optional declared neighbors.
func Load(ctx context.Context, project domain.ProjectContext, includeRelated bool, resolve ResolveProject) domain.KnowledgeSnapshot {
	result := domain.KnowledgeSnapshot{}
	loadProject(ctx, project, &result)
	if includeRelated && resolve != nil {
		cfg, err := readManifest(project.RootPath)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, err.Error())
		} else {
			seen := map[string]bool{project.Slug: true}
			for _, slug := range cfg.RelatedProjects {
				if seen[slug] {
					continue
				}
				seen[slug] = true
				neighbor, err := resolve(ctx, slug)
				if err != nil {
					result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("related project %q: %v", slug, err))
					continue
				}
				loadProject(ctx, neighbor.Context(), &result)
			}
		}
	}
	sort.Slice(result.Resources, func(i, j int) bool {
		return result.Resources[i].Project+":"+result.Resources[i].ID < result.Resources[j].Project+":"+result.Resources[j].ID
	})
	sort.Slice(result.Relations, func(i, j int) bool {
		a, b := result.Relations[i], result.Relations[j]
		return a.From+"|"+a.To+"|"+a.Kind < b.From+"|"+b.To+"|"+b.Kind
	})
	validateRelations(&result, includeRelated)
	return result
}

func loadProject(ctx context.Context, project domain.ProjectContext, result *domain.KnowledgeSnapshot) {
	if err := ctx.Err(); err != nil {
		result.Diagnostics = append(result.Diagnostics, err.Error())
		return
	}
	cfg, err := readManifest(project.RootPath)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, err.Error())
		return
	}
	for _, item := range cfg.Sources {
		if len(result.Resources) >= maxResources {
			result.Diagnostics = append(result.Diagnostics, "knowledge resource limit exceeded")
			return
		}
		path, err := safePath(project.RootPath, item.Path)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, err.Error())
			continue
		}
		switch item.Format {
		case "markdown", "okf":
			readMarkdownSource(ctx, project, item.Format, path, result)
		case "openapi":
			readOpenAPI(project, path, result)
		case "cli":
			readCLI(project, path, result)
		default:
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("unknown knowledge format %q", item.Format))
		}
	}
}

func readManifest(root string) (manifest, error) {
	path, err := safePath(root, "docs/knowledge.yaml")
	if err != nil {
		return manifest{}, err
	}
	data, err := readFile(path)
	if os.IsNotExist(err) {
		if info, statErr := os.Stat(filepath.Join(root, "docs")); statErr == nil && info.IsDir() {
			return manifest{Version: 1, Sources: []source{{Format: "markdown", Path: "docs"}}}, nil
		}
		return manifest{Version: 1}, nil
	}
	if err != nil {
		return manifest{}, fmt.Errorf("knowledge manifest: %w", err)
	}
	var cfg manifest
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("knowledge manifest: %w", err)
	}
	if cfg.Version != 1 {
		return cfg, fmt.Errorf("knowledge manifest: expected version 1")
	}
	return cfg, nil
}

func safePath(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || filepath.Clean(name) == ".." || strings.HasPrefix(filepath.Clean(name), ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("knowledge path escapes project root: %q", name)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, name)
	current := root
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("knowledge path contains a symlink: %s", current)
		}
	}
	return path, nil
}

func readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("knowledge source is not a regular file: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("knowledge source must be UTF-8 and at most 1 MiB: %s", path)
	}
	return data, nil
}

func hasResourceCapacity(result *domain.KnowledgeSnapshot, count int) bool {
	if count <= maxResources-len(result.Resources) {
		return true
	}
	result.Diagnostics = append(result.Diagnostics, "knowledge resource limit exceeded")
	return false
}

func validateRelations(result *domain.KnowledgeSnapshot, includeRelated bool) {
	known := make(map[string]bool, len(result.Resources))
	for _, item := range result.Resources {
		id := item.Project + ":" + item.ID
		if known[id] {
			result.Diagnostics = append(result.Diagnostics, "duplicate knowledge resource: "+id)
		}
		known[id] = true
	}
	for _, edge := range result.Relations {
		if !includeRelated && !strings.HasPrefix(edge.To, strings.SplitN(edge.From, ":", 2)[0]+":") {
			continue
		}
		if !known[edge.To] {
			result.Diagnostics = append(result.Diagnostics, "unresolved knowledge reference: "+edge.From+" -> "+edge.To)
		}
	}
}

func qualifiedKnowledgeLink(project, link string) (string, bool) {
	if !strings.HasPrefix(link, "okt://") {
		return project + ":" + link, link != ""
	}
	parts := strings.SplitN(strings.TrimPrefix(link, "okt://"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + ":" + parts[1], true
}

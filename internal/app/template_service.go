package app

import (
	"fmt"
	"os"
	"strings"

	"context"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TemplateService is the application-layer entry point for template
// default-binding mutations. The TUI used to inline the frontmatter rewrite
// + sibling-clearing logic into its picker; centralizing it here keeps the
// transactional sequence (snapshot → mutate → apply) close to the
// BundleEditor and gives the behavior an isolated test surface. snap is
// the per-project Snapshot captured at construction so callers that need
// to read template metadata (default binding by kind, lookup by slug)
// without round-tripping through editor.Load can do so against the same
// immutable view the rest of the app sees.
type TemplateService struct {
	snap   *config.Snapshot
	editor *BundleEditor
	files  EntityFileWriter
}

// NewTemplateService wires the template-binding service against an
// immutable per-project Snapshot. snap is required; tests that drive
// disk-only flows may pass nil but production composition always supplies
// the ProjectRuntime.Snapshot pointer so the service shares the same
// view as Persona/Skill/Law/Workflow.
func NewTemplateService(snap *config.Snapshot, editor *BundleEditor, files EntityFileWriter) *TemplateService {
	return &TemplateService{snap: snap, editor: editor, files: files}
}

// SetDefault writes `default: <kind>` (and `project: <projectSlug>` when
// non-empty) into the focused template's frontmatter, and atomically
// clears the same (kind, project) binding from any other template that
// previously held it. A single ApplyWithFiles call wraps every file edit
// + the wiring round-trip so a failure rolls everything back.
//
// Pass kind == "" to clear the binding (drops both the default and project
// frontmatter keys from the focused template).
func (s *TemplateService) SetDefault(ctx context.Context, slug, kind, projectSlug string) (err error) {
	finish := activity.Track(ctx, "app.TemplateService.SetDefault", domain.ProjectContext{Slug: projectSlug}, map[string]any{"slug": slug, "kind": kind, "project": projectSlug})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if s.editor == nil {
		err = fmt.Errorf("template service: editor not available")
		return
	}
	bundle, err := s.editor.Load()
	if err != nil {
		return
	}
	target, found := findTemplateInBundle(bundle, slug)
	if !found {
		err = fmt.Errorf("template %q not found", slug)
		return
	}

	scopeProject := projectSlug
	if kind == "" {
		scopeProject = ""
	}

	updated, err := rewriteTemplateFrontmatter(target.SourcePath, kind, scopeProject)
	if err != nil {
		return
	}
	ops := []FileOp{{Op: OpWrite, Path: target.SourcePath, Bytes: updated}}

	if kind != "" {
		for _, sibling := range bundle.Templates {
			if sibling.Slug == slug {
				continue
			}
			if sibling.Default == kind && sibling.ProjectSlug == projectSlug {
				cleared, rerr := rewriteTemplateFrontmatter(sibling.SourcePath, "", "")
				if rerr != nil {
					err = rerr
					return
				}
				ops = append(ops, FileOp{Op: OpWrite, Path: sibling.SourcePath, Bytes: cleared})
			}
		}
	}

	_, err = s.editor.ApplyWithFiles(ctx, nil, ops)
	return
}

func findTemplateInBundle(bundle config.Bundle, slug string) (config.TaskTemplate, bool) {
	for _, t := range bundle.Templates {
		if t.Slug == slug {
			return t, true
		}
	}
	return config.TaskTemplate{}, false
}

// readTemplateFile is a thin wrapper around os.ReadFile factored out for
// stubbing in tests; production reads straight from disk.
var readTemplateFile = func(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// rewriteTemplateFrontmatter loads the template file, sets/clears the
// `default:` and `project:` fields in the frontmatter, and returns the
// new file bytes. Other frontmatter keys, the body, and ordering are
// preserved so user-authored formatting survives the round-trip.
func rewriteTemplateFrontmatter(path, kind, projectSlug string) ([]byte, error) {
	raw, err := readTemplateFile(path)
	if err != nil {
		return nil, err
	}
	fm, body, err := config.SplitFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	lines := strings.Split(strings.TrimRight(string(fm), "\n"), "\n")
	wroteDefault := false
	wroteProject := false
	out := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(stripped, "default:"):
			if kind != "" {
				out = append(out, fmt.Sprintf("default: %s", kind))
				wroteDefault = true
			}
			// kind == "" → drop the line (clears the binding)
		case strings.HasPrefix(stripped, "project:"):
			if projectSlug != "" {
				out = append(out, fmt.Sprintf("project: %s", projectSlug))
				wroteProject = true
			}
		default:
			out = append(out, line)
		}
	}
	if kind != "" && !wroteDefault {
		out = append(out, fmt.Sprintf("default: %s", kind))
	}
	if projectSlug != "" && !wroteProject {
		out = append(out, fmt.Sprintf("project: %s", projectSlug))
	}

	return config.JoinFrontmatter([]byte(strings.Join(out, "\n")+"\n"), body), nil
}

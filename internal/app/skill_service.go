package app

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type SkillService struct {
	repo    ConfigRepository
	editor  *BundleEditor
	files   EntityFileWriter
	slugger Slugifier
}

func NewSkillService(repo ConfigRepository, editor *BundleEditor, files EntityFileWriter, slugger Slugifier) *SkillService {
	return &SkillService{repo: repo, editor: editor, files: files, slugger: slugger}
}

// List returns the active skill set as imported into SQLite. Description, body,
// and source path are sourced from the on-disk bundle so the response reflects
// the current state of the .md files rather than the materialized DB.
func (s *SkillService) List(ctx context.Context) ([]domain.Skill, error) {
	skills, err := s.repo.ListActiveSkills(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.editor.Load()
	if err != nil {
		return nil, err
	}
	bySlug := indexSkills(bundle.Skills)
	warnings := warningIndex(bundle.Warnings)
	for index, skill := range skills {
		if file, ok := bySlug[skill.Key]; ok {
			skills[index].Description = file.Description
			skills[index].Body = file.Body
			skills[index].SourcePath = file.SourcePath
			skills[index].Name = file.Name
		}
		if w, ok := warnings[skill.Key]; ok {
			skills[index].Warning = w
		}
	}
	return skills, nil
}

// Show returns a single skill plus its frontmatter and body. Used by the CLI
// `okt skill show` envelope.
func (s *SkillService) Show(ctx context.Context, slug string) (domain.Skill, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Skill{}, domain.NewError(domain.ErrValidation, "skill slug is required", nil)
	}
	skills, err := s.List(ctx)
	if err != nil {
		return domain.Skill{}, err
	}
	for _, skill := range skills {
		if skill.Key == slug {
			return skill, nil
		}
	}
	return domain.Skill{}, domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"slug": slug})
}

// Add creates a new skill: writes skills/custom/<slug>.md (the user-owned
// subtree, preserved across default refreshes) and adds the slug to the
// wiring file's `skills:` ref list. The on-disk file lands first
// (transactional via BundleEditor); the caller can then open $EDITOR against
// SourcePath to flesh out the body.
func (s *SkillService) Add(ctx context.Context, input domain.SkillInput) (domain.Skill, error) {
	slug, name, description, body, err := normalizeSkillInput(input, s.slugger)
	if err != nil {
		return domain.Skill{}, err
	}
	path := s.files.CustomEntityFilePath(s.editor.RootDir(), config.EntityKindSkill, slug)
	bytes, err := s.files.SkillFileBytes(config.Skill{Slug: slug, Name: name, Description: description, Body: body})
	if err != nil {
		return domain.Skill{}, configError(path, err)
	}

	if err := assertNoCollision(path, slug, "skill"); err != nil {
		return domain.Skill{}, err
	}

	if _, err := s.editor.ApplyWithFiles(ctx, func(bundle *config.Bundle) error {
		if containsString(bundleSkillSlugs(*bundle), slug) {
			return domain.NewError(domain.ErrValidation, "skill key must be unique", map[string]any{"slug": slug})
		}
		bundle.Skills = append(bundle.Skills, config.Skill{Slug: slug, Name: name, Description: description, Body: body, SourcePath: path, IsCustom: true})
		return nil
	}, []FileOp{{Op: OpWrite, Path: path, Bytes: bytes}}); err != nil {
		return domain.Skill{}, err
	}
	return s.Show(ctx, slug)
}

// Edit updates frontmatter or body of an existing skill, also rewriting the
// skills/<slug>.md file. Slug is immutable: callers wishing to rename must
// delete and re-add (this matches the file-as-source-of-truth contract).
func (s *SkillService) Edit(ctx context.Context, slug string, update domain.SkillUpdate) (domain.Skill, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Skill{}, domain.NewError(domain.ErrValidation, "skill slug is required", nil)
	}
	current, err := s.Show(ctx, slug)
	if err != nil {
		return domain.Skill{}, err
	}
	skill := config.Skill{
		Slug:        slug,
		Name:        current.Name,
		Description: current.Description,
		Body:        current.Body,
	}
	changed := false
	if update.Name != nil {
		next := strings.TrimSpace(*update.Name)
		if next == "" {
			return domain.Skill{}, domain.NewError(domain.ErrValidation, "skill name is required", nil)
		}
		skill.Name = next
		changed = true
	}
	if update.Description != nil {
		skill.Description = *update.Description
		changed = true
	}
	if update.Body != nil {
		skill.Body = *update.Body
		changed = true
	}
	if !changed {
		return current, nil
	}

	path := current.SourcePath
	if path == "" {
		// Fallback for legacy callers that didn't get an enriched SourcePath.
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindSkill, slug)
	}
	bytes, err := s.files.SkillFileBytes(skill)
	if err != nil {
		return domain.Skill{}, configError(path, err)
	}

	if _, err := s.editor.ApplyWithFiles(ctx, nil, []FileOp{{Op: OpWrite, Path: path, Bytes: bytes}}); err != nil {
		return domain.Skill{}, err
	}
	return s.Show(ctx, slug)
}

// Remove deletes the skill file and prunes the slug from `skills:` and from
// every persona's `skills:`. References are pruned silently — the requirement
// is that removing a skill does not error when a persona still references it.
func (s *SkillService) Remove(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.NewError(domain.ErrValidation, "skill slug is required", nil)
	}
	current, err := s.Show(ctx, slug)
	if err != nil {
		return err
	}
	path := current.SourcePath
	if path == "" {
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindSkill, slug)
	}

	_, err = s.editor.ApplyWithFiles(ctx, func(bundle *config.Bundle) error {
		bundle.Skills = filterSkillsBySlug(bundle.Skills, slug)
		for index := range bundle.Personas {
			bundle.Personas[index].Skills = filterStrings(bundle.Personas[index].Skills, slug)
		}
		return nil
	}, []FileOp{{Op: OpDelete, Path: path}})
	return err
}

// ScaffoldPath writes a minimal frontmatter scaffold for a new skill if the
// file does not yet exist, and returns the path so the CLI can hand it to
// $EDITOR. After the editor exits the caller must run a re-import (Apply with
// no mutations) to pick up the user's edits.
func (s *SkillService) ScaffoldPath(ctx context.Context, name string) (string, error) {
	slug := s.slugger.Slugify(name)
	if slug == "" {
		return "", domain.NewError(domain.ErrValidation, "skill name produces empty slug", map[string]any{"name": name})
	}
	added, err := s.Add(ctx, domain.SkillInput{Key: slug, Name: name})
	if err != nil {
		return "", err
	}
	return added.SourcePath, nil
}

func normalizeSkillInput(input domain.SkillInput, slugger Slugifier) (string, string, string, string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return "", "", "", "", domain.NewError(domain.ErrValidation, "skill name is required", nil)
	}
	slug := strings.TrimSpace(input.Key)
	if slug == "" {
		slug = slugger.Slugify(name)
	}
	if slug == "" {
		return "", "", "", "", domain.NewError(domain.ErrValidation, "skill key is required", nil)
	}
	if slugger.Slugify(slug) != slug {
		return "", "", "", "", domain.NewError(domain.ErrValidation, "skill key must be lowercase, hyphenated", map[string]any{"slug": slug})
	}
	return slug, name, input.Description, input.Body, nil
}

func indexSkills(items []config.Skill) map[string]config.Skill {
	out := make(map[string]config.Skill, len(items))
	for _, item := range items {
		out[item.Slug] = item
	}
	return out
}

func bundleSkillSlugs(bundle config.Bundle) []string {
	out := make([]string, 0, len(bundle.Skills))
	for _, skill := range bundle.Skills {
		out = append(out, skill.Slug)
	}
	return out
}

func filterSkillsBySlug(items []config.Skill, slug string) []config.Skill {
	out := items[:0]
	for _, item := range items {
		if item.Slug == slug {
			continue
		}
		out = append(out, item)
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

func filterStrings(items []string, drop string) []string {
	out := items[:0]
	for _, item := range items {
		if item == drop {
			continue
		}
		out = append(out, item)
	}
	return out
}

func warningIndex(warnings []config.SourceWarning) map[string]string {
	out := map[string]string{}
	for _, w := range warnings {
		if w.Slug == "" {
			continue
		}
		if _, exists := out[w.Slug]; exists {
			continue
		}
		out[w.Slug] = w.Message
	}
	return out
}

// assertNoCollision ensures we never overwrite an existing skill file when the
// user creates a "new" skill that resolves to the same slug.
func assertNoCollision(path, slug, kind string) error {
	if _, err := osStat(path); err == nil {
		return domain.NewError(domain.ErrValidation, fmt.Sprintf("%s slug already exists on disk", kind), map[string]any{"slug": slug, "path": path})
	}
	return nil
}

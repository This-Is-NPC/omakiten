package app

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type SkillService struct {
	snap    *config.Snapshot
	editor  *BundleEditor
	files   EntityFileWriter
	slugger Slugifier
}

func NewSkillService(repos EntityServiceRepos, snap *config.Snapshot) *SkillService {
	return &SkillService{snap: snap, editor: repos.Editor, files: repos.Files, slugger: repos.Slugger}
}

// skillsFromSnapshot projects the snapshot's config.Skill slice into
// the domain shape. Ids are positional (1-based) and stable within a
// snapshot; they rotate on every bundle import, so cross-rebuild
// callers must key by slug rather than by id.
func skillsFromSnapshot(snap *config.Snapshot) []domain.Skill {
	return mapSkillSlice(snap.Skills())
}

// allSkillsFromSnapshot projects the full on-disk skill catalog with the
// Active flag carried through, for the Settings catalog view.
func allSkillsFromSnapshot(snap *config.Snapshot) []domain.Skill {
	return mapSkillSlice(snap.AllSkills())
}

func mapSkillSlice(skills []config.Skill) []domain.Skill {
	out := make([]domain.Skill, 0, len(skills))
	for i, sk := range skills {
		out = append(out, domain.Skill{
			ID:          int64(i + 1),
			Key:         sk.Slug,
			Name:        sk.Name,
			Description: sk.Description,
			Body:        sk.Body,
			SourcePath:  sk.SourcePath,
			IsCustom:    sk.IsCustom,
			Active:      sk.Active,
		})
	}
	return out
}

// List returns the active skill set carried on the per-project Snapshot.
// Description, body, and source path are overlaid from the on-disk
// bundle so the response reflects the current state of the .md files —
// useful when the user edits a skill file between bundle imports.
// Always projects from the freshly-loaded bundle so a write-followed-by-read
// inside the same service instance sees the just-persisted state.
func (s *SkillService) List(_ context.Context) ([]domain.Skill, error) {
	bundle, _, _, err := s.editor.LoadPlan()
	if err != nil {
		return nil, err
	}
	return s.skillsFromBundle(bundle), nil
}

func (s *SkillService) skillsFromBundle(bundle config.Bundle) []domain.Skill {
	skills := skillsFromSnapshot(config.BuildSnapshot(bundle))
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
	return skills
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
// wiring file's `skills:` ref list. The wiring and skill files are published
// as independent whole-file atomic writes; a later failure can leave an
// earlier publication in place, and BundleEditor reports reload/retry/repair
// guidance. The caller can then open $EDITOR against SourcePath to flesh out
// the body.
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

	if err := assertNoCollision(s.files, path, slug, "skill"); err != nil {
		return domain.Skill{}, err
	}
	bundle, _, fileHashes, err := s.editor.LoadPlanWithFiles()
	if err != nil {
		return domain.Skill{}, err
	}

	if _, err := s.editor.ApplyWithFiles(ctx, bundle, fileHashes, func(bundle *config.Bundle) error {
		if containsString(bundleSkillSlugs(*bundle), slug) {
			return domain.NewError(domain.ErrValidation, "skill key must be unique", map[string]any{"slug": slug})
		}
		bundle.Skills = append(bundle.Skills, config.Skill{Slug: slug, Name: name, Description: description, Body: body, SourcePath: path, IsCustom: true})
		return nil
	}, []FileOp{{Op: OpWrite, Path: s.editor.RelativePath(path), Bytes: bytes, ExpectedHash: fileHashes[path]}}); err != nil {
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
	bundle, _, fileHashes, err := s.editor.LoadPlanWithFiles()
	if err != nil {
		return domain.Skill{}, err
	}
	var current domain.Skill
	for _, skill := range s.skillsFromBundle(bundle) {
		if skill.Key == slug {
			current = skill
			break
		}
	}
	if current.Key == "" {
		return domain.Skill{}, domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"slug": slug})
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
		// Inline-only skills (declared directly in the bundle YAML
		// without a backing .md) have no on-disk path on the snapshot.
		// Derive the conventional file path so the write lands beside
		// the other entity files.
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindSkill, slug)
	}
	bytes, err := s.files.SkillFileBytes(skill)
	if err != nil {
		return domain.Skill{}, configError(path, err)
	}

	if _, err := s.editor.ApplyWithFiles(ctx, bundle, fileHashes, nil, []FileOp{{Op: OpWrite, Path: s.editor.RelativePath(path), Bytes: bytes, ExpectedHash: fileHashes[path]}}); err != nil {
		return domain.Skill{}, err
	}
	return s.Show(ctx, slug)
}

// Remove deletes the skill file and prunes the slug from wiring references.
// References are pruned silently so removing a skill does not invalidate the
// bundle before the delete can be applied.
func (s *SkillService) Remove(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.NewError(domain.ErrValidation, "skill slug is required", nil)
	}
	bundle, _, fileHashes, err := s.editor.LoadPlanWithFiles()
	if err != nil {
		return err
	}
	var current domain.Skill
	for _, skill := range s.skillsFromBundle(bundle) {
		if skill.Key == slug {
			current = skill
			break
		}
	}
	if current.Key == "" {
		return domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"slug": slug})
	}
	path := current.SourcePath
	if path == "" {
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindSkill, slug)
	}

	_, err = s.editor.ApplyWithFiles(ctx, bundle, fileHashes, func(bundle *config.Bundle) error {
		bundle.Skills = filterSkillsBySlug(bundle.Skills, slug)
		for index := range bundle.Personas {
			bundle.Personas[index].SkillRepertoire = filterStrings(bundle.Personas[index].SkillRepertoire, slug)
		}
		for name, command := range bundle.MCPCommands {
			command.Skills = filterStrings(command.Skills, slug)
			bundle.MCPCommands[name] = command
		}
		return nil
	}, []FileOp{{Op: OpDelete, Path: s.editor.RelativePath(path), ExpectedHash: fileHashes[path]}})
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
func assertNoCollision(files EntityFileWriter, path, slug, kind string) error {
	exists, err := files.FileExists(path)
	if err != nil {
		return err
	}
	if exists {
		return domain.NewError(domain.ErrValidation, fmt.Sprintf("%s slug already exists on disk", kind), map[string]any{"slug": slug, "path": path})
	}
	return nil
}

package app

import (
	"context"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type PersonaService struct {
	snap    *config.Snapshot
	editor  *BundleEditor
	files   EntityFileWriter
	slugger Slugifier
}

func NewPersonaService(snap *config.Snapshot, editor *BundleEditor, files EntityFileWriter, slugger Slugifier) *PersonaService {
	return &PersonaService{snap: snap, editor: editor, files: files, slugger: slugger}
}

// personasFromSnapshot projects the config.Persona slice carried on
// the snapshot into the domain shape consumed by the CLI/TUI/MCP
// surfaces. Ids are positional (1-based slot in the snapshot's
// personas list) so callers that round-trip ids within a snapshot get
// stable references; ids rotate on every bundle import — callers
// that need cross-rebuild stability must key by slug, not by id.
// Skill id refs resolve against the same snapshot so the persona's
// SkillIDs are stable within the snapshot.
func personasFromSnapshot(snap *config.Snapshot) []domain.Persona {
	personas := snap.Personas()
	skills := snap.Skills()
	skillIDBySlug := make(map[string]int64, len(skills))
	for i, sk := range skills {
		skillIDBySlug[sk.Slug] = int64(i + 1)
	}
	out := make([]domain.Persona, 0, len(personas))
	for i, p := range personas {
		skillIDs := make([]int64, 0, len(p.Skills))
		for _, slug := range p.Skills {
			if id, ok := skillIDBySlug[slug]; ok {
				skillIDs = append(skillIDs, id)
			}
		}
		out = append(out, domain.Persona{
			ID:          int64(i + 1),
			Key:         p.Slug,
			Name:        p.Name,
			Description: p.Description,
			Body:        p.Body,
			SkillIDs:    skillIDs,
			SkillKeys:   append([]string(nil), p.Skills...),
			LawKeys:     append([]string(nil), p.Laws...),
			SourcePath:  p.SourcePath,
			IsCustom:    p.IsCustom,
		})
	}
	return out
}

func (s *PersonaService) List(_ context.Context) ([]domain.Persona, error) {
	bundle, err := s.editor.Load()
	if err != nil {
		return nil, err
	}
	// Always project from the on-disk bundle so a write-followed-by-read
	// inside the same service instance (Add then Edit/Show) reflects the
	// just-persisted state. The ctor-captured s.snap is still authoritative
	// for read-only fields (skill ids in resolveSkillRefs) where stable
	// positional ids matter; the listing path needs disk freshness.
	snap := config.BuildSnapshot(bundle)
	personas := personasFromSnapshot(snap)
	bySlug := indexPersonas(bundle.Personas)
	warnings := warningIndex(bundle.Warnings)
	for index, persona := range personas {
		if file, ok := bySlug[persona.Key]; ok {
			personas[index].Description = file.Description
			personas[index].Body = file.Body
			personas[index].Name = file.Name
			personas[index].SourcePath = file.SourcePath
			personas[index].LawKeys = append([]string(nil), file.Laws...)
		}
		if w, ok := warnings[persona.Key]; ok {
			personas[index].Warning = w
		}
	}
	return personas, nil
}

func (s *PersonaService) Show(ctx context.Context, slug string) (domain.Persona, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Persona{}, domain.NewError(domain.ErrValidation, "persona slug is required", nil)
	}
	personas, err := s.List(ctx)
	if err != nil {
		return domain.Persona{}, err
	}
	for _, persona := range personas {
		if persona.Key == slug {
			return persona, nil
		}
	}
	return domain.Persona{}, domain.NewError(domain.ErrPersonaNotFound, "persona not found", map[string]any{"slug": slug})
}

func (s *PersonaService) Add(ctx context.Context, input domain.PersonaInput) (domain.Persona, error) {
	slug, name, description, body, err := normalizePersonaInput(input, s.slugger)
	if err != nil {
		return domain.Persona{}, err
	}

	skillKeys, err := s.resolveSkillRefs(ctx, input)
	if err != nil {
		return domain.Persona{}, err
	}

	path := s.files.CustomEntityFilePath(s.editor.RootDir(), config.EntityKindPersona, slug)
	bytes, err := s.files.PersonaFileBytes(config.Persona{Slug: slug, Name: name, Description: description, Body: body})
	if err != nil {
		return domain.Persona{}, configError(path, err)
	}

	if err := assertNoCollision(path, slug, "persona"); err != nil {
		return domain.Persona{}, err
	}

	if _, err := s.editor.ApplyWithFiles(ctx, func(bundle *config.Bundle) error {
		for _, p := range bundle.Personas {
			if p.Slug == slug {
				return domain.NewError(domain.ErrValidation, "persona key must be unique", map[string]any{"slug": slug})
			}
		}
		// validate skill refs against the bundle on disk
		for _, ref := range skillKeys {
			if !skillRefExists(*bundle, ref) {
				return domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"slug": ref})
			}
		}
		bundle.Personas = append(bundle.Personas, config.Persona{
			Slug:        slug,
			Name:        name,
			Description: description,
			Body:        body,
			Skills:      skillKeys,
			SourcePath:  path,
			IsCustom:    true,
		})
		return nil
	}, []FileOp{{Op: OpWrite, Path: path, Bytes: bytes}}); err != nil {
		return domain.Persona{}, err
	}
	return s.Show(ctx, slug)
}

func (s *PersonaService) Edit(ctx context.Context, slug string, update domain.PersonaUpdate) (domain.Persona, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Persona{}, domain.NewError(domain.ErrValidation, "persona slug is required", nil)
	}
	current, err := s.Show(ctx, slug)
	if err != nil {
		return domain.Persona{}, err
	}

	persona := config.Persona{
		Slug:        slug,
		Name:        current.Name,
		Description: current.Description,
		Body:        current.Body,
		Skills:      append([]string(nil), current.SkillKeys...),
		Laws:        append([]string(nil), current.LawKeys...),
	}

	fileChanged := false
	if update.Name != nil {
		next := strings.TrimSpace(*update.Name)
		if next == "" {
			return domain.Persona{}, domain.NewError(domain.ErrValidation, "persona name is required", nil)
		}
		persona.Name = next
		fileChanged = true
	}
	if update.Description != nil {
		persona.Description = *update.Description
		fileChanged = true
	}
	if update.Body != nil {
		persona.Body = *update.Body
		fileChanged = true
	}

	wiringChanged := false
	if update.SkillKeys != nil || update.SkillIDs != nil {
		keys, err := s.resolveSkillRefs(ctx, domain.PersonaInput{SkillIDs: deref(update.SkillIDs), SkillKeys: deref(update.SkillKeys)})
		if err != nil {
			return domain.Persona{}, err
		}
		persona.Skills = keys
		wiringChanged = true
	}

	if !fileChanged && !wiringChanged {
		return current, nil
	}

	path := current.SourcePath
	if path == "" {
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindPersona, slug)
	}
	var ops []FileOp
	if fileChanged {
		bytes, err := s.files.PersonaFileBytes(persona)
		if err != nil {
			return domain.Persona{}, configError(path, err)
		}
		ops = append(ops, FileOp{Op: OpWrite, Path: path, Bytes: bytes})
	}

	mutator := func(bundle *config.Bundle) error {
		for index := range bundle.Personas {
			if bundle.Personas[index].Slug != slug {
				continue
			}
			if wiringChanged {
				for _, ref := range persona.Skills {
					if !skillRefExists(*bundle, ref) {
						return domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"slug": ref})
					}
				}
				bundle.Personas[index].Skills = persona.Skills
			}
			return nil
		}
		return domain.NewError(domain.ErrPersonaNotFound, "persona not found", map[string]any{"slug": slug})
	}

	if _, err := s.editor.ApplyWithFiles(ctx, mutator, ops); err != nil {
		return domain.Persona{}, err
	}
	return s.Show(ctx, slug)
}

func (s *PersonaService) Remove(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.NewError(domain.ErrValidation, "persona slug is required", nil)
	}
	current, err := s.Show(ctx, slug)
	if err != nil {
		return err
	}
	path := current.SourcePath
	if path == "" {
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindPersona, slug)
	}
	_, err = s.editor.ApplyWithFiles(ctx, func(bundle *config.Bundle) error {
		out := bundle.Personas[:0]
		for _, persona := range bundle.Personas {
			if persona.Slug == slug {
				continue
			}
			out = append(out, persona)
		}
		bundle.Personas = out
		// Also drop persona-scoped laws owned by this persona (Phase 2).
		filtered := bundle.Laws[:0]
		for _, law := range bundle.Laws {
			if law.Scope == "persona" && law.PersonaSlug == slug {
				continue
			}
			filtered = append(filtered, law)
		}
		bundle.Laws = filtered
		return nil
	}, []FileOp{{Op: OpDelete, Path: path}})
	return err
}

// resolveSkillRefs converts whichever combination of SkillIDs / SkillKeys the
// caller supplied into a deduped, validated slice of skill slugs.
func (s *PersonaService) resolveSkillRefs(_ context.Context, input domain.PersonaInput) ([]string, error) {
	out := make([]string, 0, len(input.SkillIDs)+len(input.SkillKeys))
	seen := map[string]struct{}{}
	if len(input.SkillIDs) > 0 {
		skills := s.snap.Skills()
		byID := map[int64]string{}
		for i, skill := range skills {
			byID[int64(i+1)] = skill.Slug
		}
		for _, id := range input.SkillIDs {
			key, ok := byID[id]
			if !ok {
				return nil, domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"skill_id": id})
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	for _, key := range input.SkillKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out, nil
}

func normalizePersonaInput(input domain.PersonaInput, slugger Slugifier) (string, string, string, string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return "", "", "", "", domain.NewError(domain.ErrValidation, "persona name is required", nil)
	}
	slug := strings.TrimSpace(input.Key)
	if slug == "" {
		slug = slugger.Slugify(name)
	}
	if slug == "" {
		return "", "", "", "", domain.NewError(domain.ErrValidation, "persona key is required", nil)
	}
	if slugger.Slugify(slug) != slug {
		return "", "", "", "", domain.NewError(domain.ErrValidation, "persona key must be lowercase, hyphenated", map[string]any{"slug": slug})
	}
	return slug, name, input.Description, input.Body, nil
}

func indexPersonas(items []config.Persona) map[string]config.Persona {
	out := make(map[string]config.Persona, len(items))
	for _, item := range items {
		out[item.Slug] = item
	}
	return out
}

func skillRefExists(bundle config.Bundle, slug string) bool {
	for _, skill := range bundle.Skills {
		if skill.Slug == slug {
			return true
		}
	}
	return false
}

func deref[T any](p *[]T) []T {
	if p == nil {
		return nil
	}
	return *p
}

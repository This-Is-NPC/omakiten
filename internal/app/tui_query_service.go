package app

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TUISnapshot is the coherent read-model the TUI renders from. It bundles
// every collection a refresh tick needs into one struct so the consumer
// (internal/tui/model.go's refresh) does not have to fan out to a half-
// dozen repositories itself.
type TUISnapshot struct {
	Tasks        []domain.Task
	Workflow     domain.Workflow
	Dependencies []domain.TaskDependency
	Comments     []domain.Comment
	Laws         []domain.Law
	Skills       []domain.Skill
	Personas     []domain.Persona
	Templates    []config.TaskTemplate
	Entries      []domain.ContextEntry
	Settings     domain.ContextSettings
	AllTags      []domain.Tag
	TaskTagsByID map[int64][]domain.Tag
}

// TUIQueryService centralises the multi-repo read fan-out the TUI used to
// inline in its refresh method. Pulling it here keeps the TUI free of
// per-port plumbing and gives the read pipeline a single test seam.
type TUIQueryService struct {
	tasks    TaskRepository
	config   ConfigRepository
	deps     DependencyRepository
	comments CommentRepository
	entries  ContextEntryRepository
	tags     TagRepository
	editor   *BundleEditor
}

func NewTUIQueryService(tasks TaskRepository, cfg ConfigRepository, deps DependencyRepository, comments CommentRepository, entries ContextEntryRepository, tags TagRepository, editor *BundleEditor) *TUIQueryService {
	return &TUIQueryService{tasks: tasks, config: cfg, deps: deps, comments: comments, entries: entries, tags: tags, editor: editor}
}

// Snapshot loads every collection the TUI needs to render the board, the
// activity feeds and the entity views in one shot. Order: tasks first
// (most likely to fail with a fresh empty store), workflow, dependencies,
// comments, the entity collections (with on-disk enrichment when the
// editor is available), context entries + settings, then tag indexes.
func (s *TUIQueryService) Snapshot(ctx context.Context, project domain.ProjectContext, sort domain.TaskSort) (TUISnapshot, error) {
	snap := TUISnapshot{TaskTagsByID: map[int64][]domain.Tag{}}

	tasks, err := s.tasks.ListTasks(ctx, project.ID, domain.TaskFilter{Sort: sort})
	if err != nil {
		return snap, err
	}
	snap.Tasks = tasks

	workflow, err := s.config.ActiveWorkflow(ctx)
	if err != nil {
		return snap, err
	}
	snap.Workflow = workflow

	deps, err := s.deps.ListTaskDependencies(ctx, project.ID, 0)
	if err != nil {
		return snap, err
	}
	snap.Dependencies = deps

	comments, err := s.comments.ListComments(ctx, project.ID, 0)
	if err != nil {
		return snap, err
	}
	snap.Comments = comments

	laws, err := s.config.ListActiveLaws(ctx)
	if err != nil {
		return snap, err
	}
	skills, err := s.config.ListActiveSkills(ctx)
	if err != nil {
		return snap, err
	}
	personas, err := s.config.ListActivePersonas(ctx)
	if err != nil {
		return snap, err
	}

	if s.editor != nil {
		bundle, err := s.editor.Load()
		if err != nil {
			return snap, err
		}
		skills = enrichSkillsFromBundle(skills, bundle)
		laws = enrichLawsFromBundle(laws, bundle)
		personas = enrichPersonasFromBundle(personas, bundle)
		snap.Templates = append([]config.TaskTemplate(nil), bundle.Templates...)
	}
	snap.Laws = laws
	snap.Skills = skills
	snap.Personas = personas

	entries, err := s.entries.ListContextEntries(ctx, project.ID)
	if err != nil {
		return snap, err
	}
	snap.Entries = entries

	settings, err := s.config.ContextSettings(ctx)
	if err != nil {
		return snap, err
	}
	snap.Settings = settings

	if s.tags != nil {
		allTags, err := s.tags.ListAllTags(ctx)
		if err != nil {
			return snap, err
		}
		snap.AllTags = allTags
		taskTagsMap, err := s.tags.ListTaskTagsByProject(ctx, project.ID)
		if err != nil {
			return snap, err
		}
		snap.TaskTagsByID = taskTagsMap
	}

	return snap, nil
}

// bundleWarningIndex returns the first source-warning message keyed by
// slug. Mirrors what the CLI's `list` paths report so the TUI surfaces the
// same non-fatal issues.
func bundleWarningIndex(warnings []config.SourceWarning) map[string]string {
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

func enrichSkillsFromBundle(skills []domain.Skill, bundle config.Bundle) []domain.Skill {
	bySlug := map[string]config.Skill{}
	for _, skill := range bundle.Skills {
		bySlug[skill.Slug] = skill
	}
	warnings := bundleWarningIndex(bundle.Warnings)
	for index, skill := range skills {
		if file, ok := bySlug[skill.Key]; ok {
			skills[index].Description = file.Description
			skills[index].Body = file.Body
			skills[index].SourcePath = file.SourcePath
			skills[index].IsCustom = file.IsCustom
			if file.Name != "" {
				skills[index].Name = file.Name
			}
		}
		if w, ok := warnings[skill.Key]; ok {
			skills[index].Warning = w
		}
	}
	return skills
}

func enrichLawsFromBundle(laws []domain.Law, bundle config.Bundle) []domain.Law {
	bySlug := map[string]config.Law{}
	for _, law := range bundle.Laws {
		bySlug[law.Slug] = law
	}
	warnings := bundleWarningIndex(bundle.Warnings)
	for index, law := range laws {
		if file, ok := bySlug[law.Key]; ok {
			laws[index].Body = file.Body
			laws[index].Severity = file.Severity
			laws[index].SourcePath = file.SourcePath
			laws[index].Scope = domain.LawScope(file.Scope)
			laws[index].ProjectKey = file.ProjectSlug
			laws[index].PersonaKey = file.PersonaSlug
			laws[index].IsCustom = file.IsCustom
			if file.Name != "" {
				laws[index].Name = file.Name
			}
		}
		if w, ok := warnings[law.Key]; ok {
			laws[index].Warning = w
		}
	}
	return laws
}

func enrichPersonasFromBundle(personas []domain.Persona, bundle config.Bundle) []domain.Persona {
	bySlug := map[string]config.Persona{}
	for _, persona := range bundle.Personas {
		bySlug[persona.Slug] = persona
	}
	warnings := bundleWarningIndex(bundle.Warnings)
	for index, persona := range personas {
		if file, ok := bySlug[persona.Key]; ok {
			personas[index].Description = file.Description
			personas[index].Body = file.Body
			personas[index].SourcePath = file.SourcePath
			personas[index].LawKeys = append([]string(nil), file.Laws...)
			personas[index].IsCustom = file.IsCustom
			if file.Name != "" {
				personas[index].Name = file.Name
			}
		}
		if w, ok := warnings[persona.Key]; ok {
			personas[index].Warning = w
		}
	}
	return personas
}

package studio

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"omakiten/internal/commandcatalog"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// writeStudioBenchEntities fills the two gaps SaveFullBundle leaves. It does
// not serialise templates at all, and config.PersonaFileBytes writes only name
// + description — so a bundle saved and reloaded through it would come back
// with no templates and empty skill repertoires, and the validator would reject
// every command that binds a skill. Both are exactly the binding shape the
// benchmark and golden fixtures need to price.
func writeStudioBenchEntities(configPath string, bundle config.Bundle) error {
	root := config.ConfigRootFromYAMLPath(configPath)
	for _, persona := range bundle.Personas {
		schema := persona.SchemaVersion
		if schema == 0 {
			schema = 2
		}
		fm, err := yaml.Marshal(struct {
			Name            string   `yaml:"name"`
			Description     string   `yaml:"description,omitempty"`
			SchemaVersion   int      `yaml:"schema_version"`
			SkillRepertoire []string `yaml:"skill_repertoire,omitempty"`
			Laws            []string `yaml:"laws,omitempty"`
		}{
			Name:            persona.Name,
			Description:     persona.Description,
			SchemaVersion:   schema,
			SkillRepertoire: persona.SkillRepertoire,
			Laws:            persona.Laws,
		})
		if err != nil {
			return fmt.Errorf("persona %s frontmatter: %w", persona.Slug, err)
		}
		path := config.EntityFilePath(root, config.EntityKindPersona, persona.Slug)
		if err := config.WriteAtomic(path, config.JoinFrontmatter(fm, []byte(persona.Body))); err != nil {
			return fmt.Errorf("write persona %s: %w", persona.Slug, err)
		}
	}
	for _, template := range bundle.Templates {
		var fm strings.Builder
		fmt.Fprintf(&fm, "name: %s\ndescription: %s\nentity: %s\n", template.Name, template.Description, template.Entity)
		if template.Default != "" {
			fmt.Fprintf(&fm, "default: %s\n", template.Default)
		}
		path := config.EntityFilePath(root, config.EntityKindTemplate, template.Slug)
		if err := config.WriteAtomic(path, config.JoinFrontmatter([]byte(fm.String()), []byte(template.Body))); err != nil {
			return fmt.Errorf("write template %s: %w", template.Slug, err)
		}
	}
	return nil
}

func studioBenchWorkflow() domain.Workflow {
	return domain.Workflow{ID: 1, Key: "omakase", Name: "Omakase Workflow", Buckets: []domain.Bucket{
		{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
		{ID: 2, Key: "dev", Name: "Development", Position: 2},
		{ID: 3, Key: "review", Name: "Review", Position: 3},
		{ID: 4, Key: "done", Name: "Done", Position: 4}}, Transitions: []domain.WorkflowTransition{
		{FromBucketID: 1, ToBucketID: 2},
		{FromBucketID: 2, ToBucketID: 3},
		{FromBucketID: 3, ToBucketID: 4}}}
}

func studioBenchTasks(n int) []domain.Task {
	buckets := []string{"backlog", "dev", "review", "done"}
	tasks := make([]domain.Task, 0, n)
	for i := 0; i < n; i++ {
		tasks = append(tasks, domain.Task{ID: int64(i + 1), Title: fmt.Sprintf("Task %d", i+1), BucketKey: buckets[i%len(buckets)]})
	}
	return tasks
}

// studioBenchBody is the prose body every persona, skill, law and template in
// the fixture carries. Report() clones each of these twice and DiffStudioBundles
// walks them again, so a fixture with empty bodies would measure an empty map
// rather than a configuration bundle.
func studioBenchBody(kind, slug string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s %s\n\n", strings.ToUpper(kind), slug)
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&sb, "Paragraph %d of the %s %q body, carrying enough prose that cloning and diffing it costs what a real bundle costs.\n\n", i, kind, slug)
	}
	return sb.String()
}

// studioBenchBundle mirrors the shape of a shipped Omakiten bundle: every known
// playbook bound, plus the persona/skill/law/template catalogs those
// bindings resolve against.
func studioBenchBundle() config.Bundle {
	const (
		personaCount  = 12
		skillCount    = 32
		lawCount      = 20
		templateCount = 12
	)

	skills := make([]config.Skill, 0, skillCount)
	skillSlugs := make([]string, 0, skillCount)
	for i := 0; i < skillCount; i++ {
		slug, name := fmt.Sprintf("skill-%02d", i), fmt.Sprintf("Skill %02d", i)
		skillSlugs = append(skillSlugs, slug)
		skills = append(skills, config.Skill{Slug: slug, Name: name, Description: "Bench skill " + slug, Body: studioBenchBody("skill", slug)})
	}

	laws := make([]config.Law, 0, lawCount)
	lawSlugs := make([]string, 0, lawCount)
	for i := 0; i < lawCount; i++ {
		slug, name := fmt.Sprintf("law-%02d", i), fmt.Sprintf("Law %02d", i)
		lawSlugs = append(lawSlugs, slug)
		laws = append(laws, config.Law{Slug: slug, Name: name, Severity: "error", Scope: "global", Body: studioBenchBody("law", slug)})
	}

	personas := make([]config.Persona, 0, personaCount)
	for i := 0; i < personaCount; i++ {
		slug, name := fmt.Sprintf("persona-%02d", i), fmt.Sprintf("Persona %02d", i)
		repertoire := skillSlugs[i%len(skillSlugs):]
		if len(repertoire) > 6 {
			repertoire = repertoire[:6]
		}
		personas = append(personas, config.Persona{
			Slug: slug, Name: name, Description: "Bench persona " + slug,
			Body: studioBenchBody("persona", slug), SkillRepertoire: append([]string(nil), repertoire...)})
	}

	templates := make([]config.TaskTemplate, 0, templateCount)
	templateSlugs := make([]string, 0, templateCount)
	for i := 0; i < templateCount; i++ {
		slug, name := fmt.Sprintf("template-%02d", i), fmt.Sprintf("Template %02d", i)
		templateSlugs = append(templateSlugs, slug)
		template := config.TaskTemplate{Slug: slug, Name: name, Description: "Bench template " + slug, Entity: "task", Body: studioBenchBody("template", slug)}
		if i == 0 {
			template.Default = "task"
		}
		templates = append(templates, template)
	}

	commands := map[string]config.CommandSpec{
		config.CommandsGlobalKey: {Laws: append([]string(nil), lawSlugs[:3]...)}}
	for i, name := range commandcatalog.CommandNames() {
		persona := personas[i%len(personas)]
		// A command may only bind skills the persona actually carries in its
		// repertoire; the bundle validator rejects anything else.
		skills := persona.SkillRepertoire
		if len(skills) > 3 {
			skills = skills[:3]
		}
		commands[name] = config.CommandSpec{
			Persona:   persona.Slug,
			Skills:    append([]string(nil), skills...),
			Laws:      append([]string(nil), lawSlugs[i%len(lawSlugs):min(i%len(lawSlugs)+4, len(lawSlugs))]...),
			Templates: append([]string(nil), templateSlugs[i%len(templateSlugs):min(i%len(templateSlugs)+2, len(templateSlugs))]...)}
	}

	tru := true
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "omakase", Name: "Omakase"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Workflow: config.WorkflowSettings{Active: "omakase"},
			Theme:    config.ThemeSettings{Active: "default"},
			Agent: config.AgentSettings{
				RecentCommentLimit:        5,
				IncludeWorkflowInContinue: &tru,
				NextWorkLimit:             5,
				SimilarTaskLimit:          5},
			TUI:              config.TUISettings{TokenBadge: config.TokenBadgeThresholds{YellowAt: 150, RedAt: 400}},
			TemplateDefaults: []string{"task"},
			Priorities: []config.PriorityDefinition{
				{ID: 1, Value: "low"},
				{ID: 2, Value: "normal", Default: true},
				{ID: 3, Value: "high"}},
			Severities: []config.SeverityDefinition{
				{ID: 1, Value: "info"},
				{ID: 2, Value: "warning", Default: true},
				{ID: 3, Value: "error"}},
			Views: config.ViewSettings{
				Board:        config.BoardViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Table:        config.TableViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Graph:        config.GraphViewSettings{Sort: config.SortSettings{Field: "id", Order: "asc"}},
				Logs:         config.LogsViewSettings{Sort: config.SortSettings{Order: "desc"}, Limit: 50, WindowDays: 30},
				TaskActivity: config.TaskActivityViewSettings{Sort: config.SortSettings{Order: "asc"}}},
			SQLite:    config.SQLiteSettings{BusyTimeoutMs: 5000, CacheSizeKB: 1024},
			Solutions: config.SolutionsSettings{DefaultTopLimit: 10, MaxTopLimit: 100},
			Backup:    config.BackupSettings{RetentionCount: 5},
			Events: config.EventsSettings{
				DefaultRecentLimit: 50,
				Defaults:           config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru}},
			Search:      config.SearchSettings{Stopwords: []string{"and", "the"}},
			TagSynonyms: map[string]string{"golang": "go"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "omakase",
			Name: "Omakase Workflow",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
				{ID: 3, Key: "review", Name: "Review", Position: 3},
				{ID: 4, Key: "done", Name: "Done", Position: 4}},
			Transitions: []config.Transition{{From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 4}}}},
		Skills:       skills,
		AllSkills:    skills,
		Laws:         laws,
		AllLaws:      laws,
		Personas:     personas,
		AllPersonas:  personas,
		Templates:    templates,
		AllTemplates: templates,
		Commands:     commands,
		Surfaces:     config.CanonicalSurfaceTable(),
	}
}

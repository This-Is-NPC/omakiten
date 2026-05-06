package tui

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func (m Model) findLawBySlug(slug string) (domain.Law, bool) {
	for _, law := range m.laws {
		if law.Key == slug {
			return law, true
		}
	}
	return domain.Law{}, false
}

func (m Model) findSkillBySlug(slug string) (domain.Skill, bool) {
	for _, skill := range m.skills {
		if skill.Key == slug {
			return skill, true
		}
	}
	return domain.Skill{}, false
}

func (m Model) findPersonaBySlug(slug string) (domain.Persona, bool) {
	for _, persona := range m.personas {
		if persona.Key == slug {
			return persona, true
		}
	}
	return domain.Persona{}, false
}

func (m Model) findTemplateBySlug(slug string) (config.TaskTemplate, bool) {
	for _, template := range m.templates {
		if template.Slug == slug {
			return template, true
		}
	}
	return config.TaskTemplate{}, false
}

// nextScaffoldName picks a unique placeholder name like "New skill 1" so that
// the user can rename it inside $EDITOR. The slug derives from the chosen name.
func nextScaffoldName(kind entityKind, m Model) string {
	prefix := "New " + strings.ToLower(kind.String())
	existing := map[string]struct{}{}
	switch kind {
	case entityKindLaw:
		for _, law := range m.laws {
			existing[law.Key] = struct{}{}
		}
	case entityKindSkill:
		for _, skill := range m.skills {
			existing[skill.Key] = struct{}{}
		}
	case entityKindPersona:
		for _, persona := range m.personas {
			existing[persona.Key] = struct{}{}
		}
	}
	for n := 1; n < 1000; n++ {
		candidate := fmt.Sprintf("%s %d", prefix, n)
		slug := slugFromName(candidate)
		if _, taken := existing[slug]; !taken {
			return candidate
		}
	}
	return prefix
}

// slugFromName mirrors config.Slugify without forcing a config import inside
// the TUI hot path.
func slugFromName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// scaffoldEntity calls into the appropriate service to create a placeholder
// entity file and returns its absolute path so the TUI can hand it to $EDITOR.
func scaffoldEntity(ctx context.Context, kind entityKind, repos Repositories, name string) (string, error) {
	switch kind {
	case entityKindSkill:
		service := app.NewSkillService(repos.Config, repos.Editor, repos.EntityFiles, repos.Slugger)
		skill, err := service.Add(ctx, domain.SkillInput{Name: name})
		if err != nil {
			return "", err
		}
		return skill.SourcePath, nil
	case entityKindLaw:
		service := app.NewLawService(repos.Config, repos.Editor, repos.EntityFiles, repos.Slugger)
		law, err := service.Add(ctx, domain.LawInput{
			Key:      slugFromName(name),
			Name:     name,
			Severity: domain.LawSeverityError,
			Body:     "TODO: write the law body.",
		})
		if err != nil {
			return "", err
		}
		return law.SourcePath, nil
	case entityKindPersona:
		service := app.NewPersonaService(repos.Config, repos.Editor, repos.EntityFiles, repos.Slugger)
		persona, err := service.Add(ctx, domain.PersonaInput{Name: name})
		if err != nil {
			return "", err
		}
		return persona.SourcePath, nil
	}
	return "", fmt.Errorf("unknown entity kind")
}

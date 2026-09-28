package tui

import (
	"context"
	"fmt"
	"strings"

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
		slug := domain.Slugify(candidate)
		if _, taken := existing[slug]; !taken {
			return candidate
		}
	}
	return prefix
}

// defaultSeverityID returns the configured default severity from the Model's
// loaded severities slice, falling back to the midpoint entry or SeverityZero
// so that law scaffolding never calls the process-global domain registries.
func (m Model) defaultSeverityID() domain.Severity {
	for _, s := range m.severities {
		if s.Default {
			return domain.Severity(s.ID)
		}
	}
	if len(m.severities) > 0 {
		return domain.Severity(m.severities[len(m.severities)/2].ID)
	}
	return domain.SeverityZero
}

// scaffoldEntity calls into the appropriate service to create a placeholder
// entity file and returns its absolute path so the TUI can hand it to $EDITOR.
func (m Model) scaffoldEntity(ctx context.Context, kind entityKind, repos Repositories, name string) (string, error) {
	svc := repos.operationService()
	if svc == nil {
		return "", fmt.Errorf("operation service is not wired")
	}
	switch kind {
	case entityKindSkill:
		skill, err := svc.AddSkill(ctx, domain.SkillInput{Name: name})
		if err != nil {
			return "", err
		}
		return skill.SourcePath, nil
	case entityKindLaw:
		severityID := m.defaultSeverityID()
		law, err := svc.AddLaw(ctx, domain.LawInput{
			Key:      domain.Slugify(name),
			Name:     name,
			Severity: severityID,
			Body:     "TODO: write the law body."})
		if err != nil {
			return "", err
		}
		return law.SourcePath, nil
	case entityKindPersona:
		persona, err := svc.AddPersona(ctx, domain.PersonaInput{Name: name})
		if err != nil {
			return "", err
		}
		return persona.SourcePath, nil
	}
	return "", fmt.Errorf("unknown entity kind")
}

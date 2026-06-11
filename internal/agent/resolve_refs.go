package agent

import (
	"omakiten/internal/domain"
)

// resolveLawSlugs expands law slug references into full LawInfo rows using the
// injected catalog. Unknown slugs reject with validation_error naming the slug.
func resolveLawSlugs(slugs []string, catalog LawCatalog) ([]LawInfo, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	if catalog == nil {
		return nil, domain.NewError(domain.ErrValidation, "law catalog not initialized", nil)
	}
	bySlug := make(map[string]LawInfo, len(slugs))
	for _, law := range catalog() {
		bySlug[law.Slug] = law
	}
	out := make([]LawInfo, 0, len(slugs))
	for _, slug := range slugs {
		law, ok := bySlug[slug]
		if !ok {
			return nil, domain.NewError(domain.ErrValidation, "law not found", map[string]any{"slug": slug})
		}
		out = append(out, law)
	}
	return out, nil
}

// resolveSkillSlugs expands skill slug references into SkillSummary rows with
// bodies using the injected catalog. Unknown slugs reject with validation_error
// naming the missing slug.
func resolveSkillSlugs(slugs []string, catalog SkillCatalog) ([]SkillSummary, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	if catalog == nil {
		return nil, domain.NewError(domain.ErrValidation, "skill catalog not initialized", nil)
	}
	bySlug := make(map[string]SkillInfo, len(slugs))
	for _, sk := range catalog() {
		bySlug[sk.Slug] = sk
	}
	out := make([]SkillSummary, 0, len(slugs))
	for _, slug := range slugs {
		sk, ok := bySlug[slug]
		if !ok {
			return nil, domain.NewError(domain.ErrValidation, "skill not found", map[string]any{"slug": slug})
		}
		out = append(out, SkillSummary(sk))
	}
	return out, nil
}

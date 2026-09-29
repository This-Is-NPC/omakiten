package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// resolveLawSlugs expands law slug references into full LawInfo rows using the
// injected catalog. Unknown slugs return the corresponding not-found error.
func resolveLawSlugs(slugs []string, catalog contract.LawCatalog) ([]contract.LawInfo, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	if catalog == nil {
		return nil, domain.NewError(domain.ErrValidation, "law catalog not initialized", nil)
	}
	bySlug := make(map[string]contract.LawInfo, len(slugs))
	for _, law := range catalog() {
		bySlug[law.Slug] = law
	}
	out := make([]contract.LawInfo, 0, len(slugs))
	for _, slug := range slugs {
		law, ok := bySlug[slug]
		if !ok {
			return nil, domain.NewError(domain.ErrLawNotFound, "law not found", map[string]any{"slug": slug})
		}
		out = append(out, law)
	}
	return out, nil
}

// resolveSkillSlugs expands skill slug references into SkillSummary rows with
// bodies using the injected catalog. Unknown slugs return ErrSkillNotFound.
func resolveSkillSlugs(slugs []string, catalog contract.SkillCatalog) ([]contract.SkillSummary, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	if catalog == nil {
		return nil, domain.NewError(domain.ErrValidation, "skill catalog not initialized", nil)
	}
	bySlug := make(map[string]contract.SkillInfo, len(slugs))
	for _, sk := range catalog() {
		bySlug[sk.Slug] = sk
	}
	out := make([]contract.SkillSummary, 0, len(slugs))
	for _, slug := range slugs {
		sk, ok := bySlug[slug]
		if !ok {
			return nil, domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"slug": slug})
		}
		out = append(out, contract.SkillSummary(sk))
	}
	return out, nil
}

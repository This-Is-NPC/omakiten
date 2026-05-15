package sqlite

import (
	"context"

	"omakiten/internal/domain"
)

// ListActiveLaws delegates to the in-memory provider snapshot. The
// SQL `laws` table was dropped in migration 020; ids are now
// synthesised from the law's slot in the bundle (positional, 1-based),
// matching the legacy `laws.local_id`. The severity label is resolved
// to its configured id at delegation time so callers observe the same
// id↔value mapping as before — the LawService still overlays
// frontmatter values from the bundle editor for body/severity drift.
func (s *Store) ListActiveLaws(ctx context.Context) ([]domain.Law, error) {
	providers := s.Providers()
	laws := providers.Laws()
	out := make([]domain.Law, 0, len(laws))
	for i, l := range laws {
		severityID := providers.SeverityIDByLabel(l.Severity)
		out = append(out, domain.Law{
			ID:         int64(i + 1),
			Key:        l.Slug,
			Name:       l.Name,
			Severity:   domain.Severity(severityID),
			Body:       l.Body,
			Scope:      domain.LawScope(l.Scope),
			ProjectKey: l.ProjectSlug,
			PersonaKey: l.PersonaSlug,
			SourcePath: l.SourcePath,
			IsCustom:   l.IsCustom,
		})
	}
	return out, nil
}

package sqlite

import (
	"context"

	"omakiten/internal/domain"
)

func (s *Store) ListActiveLaws(ctx context.Context) ([]domain.Law, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT laws.id, laws.key, laws.severity_id, laws.body
FROM laws
JOIN config_bundles ON config_bundles.id = laws.bundle_id
WHERE laws.active = 1 AND config_bundles.active = 1
ORDER BY laws.local_id
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var laws []domain.Law
	for rows.Next() {
		var law domain.Law
		if err := rows.Scan(&law.ID, &law.Key, &law.Severity, &law.Body); err != nil {
			return nil, err
		}
		laws = append(laws, law)
	}
	return laws, rows.Err()
}

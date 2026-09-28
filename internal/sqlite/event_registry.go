package sqlite

import (
	"sort"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func (s *Store) eventRegistry(projectID int64) *domain.EventRegistry {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if r := s.eventRegistries[projectID]; r != nil {
		return r
	}
	return s.eventRegistries[0]
}
func defaultEventRegistries() map[int64]*domain.EventRegistry {
	r, err := config.BuildEventRegistry(config.MustLoadKitConfig().Events)
	if err != nil {
		panic(err)
	}
	return map[int64]*domain.EventRegistry{0: r}
}

type eventRegistryScope struct {
	registry  *domain.EventRegistry
	projectID int64
	excluded  []int64
}

func (s *Store) registryScopes() []eventRegistryScope {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	ids := make([]int64, 0, len(s.eventRegistries))
	for id := range s.eventRegistries {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	scopes := []eventRegistryScope{{registry: s.eventRegistries[0], excluded: ids}}
	for _, id := range ids {
		scopes = append(scopes, eventRegistryScope{registry: s.eventRegistries[id], projectID: id})
	}
	return scopes
}

func (scope eventRegistryScope) condition(prefix string) (string, []any) {
	column := prefix + "project_id"
	if scope.projectID > 0 {
		return column + " = ?", []any{scope.projectID}
	}
	if len(scope.excluded) == 0 {
		return "1", nil
	}
	placeholders := make([]string, len(scope.excluded))
	args := make([]any, len(scope.excluded))
	for i, id := range scope.excluded {
		placeholders[i] = "?"
		args[i] = id
	}
	return "COALESCE(" + column + ", 0) NOT IN (" + strings.Join(placeholders, ",") + ")", args
}

func (s *Store) categoryCondition(categories []domain.EventCategory) (string, []any) {
	var conditions []string
	var args []any
	for _, scope := range s.registryScopes() {
		types := eventTypesForCategories(categories, scope.registry)
		if len(types) == 0 {
			continue
		}
		condition, projectArgs := scope.condition("")
		placeholders := make([]string, len(types))
		args = append(args, projectArgs...)
		for i, eventType := range types {
			placeholders[i] = "?"
			args = append(args, eventType)
		}
		conditions = append(conditions, "("+condition+" AND event_type IN ("+strings.Join(placeholders, ",")+"))")
	}
	if len(conditions) == 0 {
		return "0", nil
	}
	return "(" + strings.Join(conditions, " OR ") + ")", args
}

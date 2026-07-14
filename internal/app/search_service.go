package app

import (
	"context"
	"encoding/json"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// SearchService is the application-layer entry for the unified FTS5
// `search` MCP tool. It validates the caller's entity-type filter
// against the closed set declared in domain.AllSearchEntityTypes,
// trims the query, and delegates the heavy lifting (BM25 ranking,
// snippet rendering, implicit task-state filter) to the adapter behind
// SearchRepository. Empty `entityTypes` is treated as "all five".
//
// When the search covers errors (entity types include "error" or the
// filter is empty), the service emits the `errors.researched` domain
// event so /metrics.summary keeps producing the "search before record"
// ratio per AI model.
type SearchService struct {
	repo   SearchRepository
	events EventRepository
}

// NewSearchService wires the service with the unified-index adapter
// plus the event recorder used for metrics emission. SQLite owns the single
// result cap so every repository caller receives the same bounded response.
func NewSearchService(repo SearchRepository, events EventRepository) *SearchService {
	return &SearchService{repo: repo, events: events}
}

// Search runs the FTS5 query under the per-project (or cross-project)
// filter. project.ID == 0 means cross-project — the service does not
// resolve the slug here; the agent layer translates project / project_id
// into a domain.ProjectContext before calling in.
func (s *SearchService) Search(ctx context.Context, project domain.ProjectContext, query string, entityTypes []string) (hits []domain.SearchHit, err error) {
	cleanQuery, err := domain.ValidateSearchQuery(query)
	if err != nil {
		return nil, err
	}
	typed, err := normalizeEntityTypes(entityTypes)
	if err != nil {
		return nil, err
	}
	finish := activity.Track(ctx, "app.SearchService.Search", project, map[string]any{
		"query":        cleanQuery,
		"entity_types": entityTypeNames(typed),
	})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	hits, err = s.repo.Search(ctx, cleanQuery, project.ID, typed)
	if err != nil {
		return
	}
	if s.events != nil && includesErrorEntity(typed) {
		payload, marshalErr := json.Marshal(map[string]any{
			"query":        cleanQuery,
			"entity_types": entityTypeNames(typed),
			"result_count": len(hits),
			"unified":      true,
		})
		if marshalErr == nil {
			_ = s.events.RecordEntityEvent(ctx, "search", 0, project.ID, domain.EventTypeErrorsResearched, string(payload))
		}
	}
	return
}

// includesErrorEntity reports whether the filter set covers errors —
// either via an explicit "error" entry or by being empty (which means
// "all five entity types").
func includesErrorEntity(types []domain.SearchEntityType) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if t == domain.SearchEntityError {
			return true
		}
	}
	return false
}

// entityTypeNames stringifies the typed slice for the JSON event payload.
// Empty input yields nil so the payload omits the field via omitempty
// semantics on the consumer side.
func entityTypeNames(types []domain.SearchEntityType) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return out
}

// normalizeEntityTypes deduplicates and validates the caller-supplied
// list. Empty input returns a nil slice — the adapter treats nil as
// "all entity types". Unknown values surface a coded validation_error
// with the offending name so callers see which input was wrong.
func normalizeEntityTypes(raw []string) ([]domain.SearchEntityType, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	capacity := min(len(raw), len(domain.AllSearchEntityTypes()))
	seen := make(map[domain.SearchEntityType]struct{}, capacity)
	out := make([]domain.SearchEntityType, 0, capacity)
	for _, v := range raw {
		name := strings.TrimSpace(v)
		if name == "" {
			continue
		}
		if !domain.IsValidSearchEntityType(name) {
			return nil, domain.NewError(domain.ErrValidation, "invalid entity_type", map[string]any{
				"value":   name,
				"allowed": entityTypeNames(domain.AllSearchEntityTypes()),
			})
		}
		t := domain.SearchEntityType(name)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out, nil
}

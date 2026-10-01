package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// TagOperations reads and changes the tags of tasks, errors, and projects.
type TagOperations interface {
	ListTags(ctx context.Context, input contract.ListTagsInput) (contract.TagListResponse, error)
}

var (
	entityTypeParam = param{name: "entity_type", in: "query", description: "`task`, `error`, or `project`.", schema: stringSchema, required: true}
	entityIDParam   = queryParam("entity_id", "Task or error id; omitted for `project`.", idSchema)
)

func (s *Server) tagRoutes() []route {
	return []route{
		query("listTags", http.MethodGet, projectPath+"/tags", "tag.list", "Tags of a task, an error, or the project.", []param{projectParam, entityTypeParam, entityIDParam}, s.listTags),
	}
}

func (s *Server) listTags(r *http.Request) (contract.TagListResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.TagListResponse{}, err
	}
	entityID, err := tagEntityID(r)
	if err != nil {
		return contract.TagListResponse{}, err
	}
	return ops.ListTags(r.Context(), contract.ListTagsInput{ProjectSelector: selector, EntityType: r.URL.Query().Get("entity_type"), EntityID: entityID})
}

// tagEntityID reads the optional entity_id query parameter.
func tagEntityID(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("entity_id")
	if raw == "" {
		return 0, nil
	}
	return parseID(raw, "entity_id")
}

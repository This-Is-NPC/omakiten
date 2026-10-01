package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// TagOperations reads and changes the tags of tasks, errors, and projects.
type TagOperations interface {
	ListTags(ctx context.Context, input contract.ListTagsInput) (contract.TagListResponse, error)
	ListAllTags(ctx context.Context) (contract.AllTagsResponse, error)
	AddTag(ctx context.Context, input contract.AddTagInput) (contract.TagResponse, error)
	RemoveTag(ctx context.Context, input contract.RemoveTagInput) (contract.RemoveTagResponse, error)
	MergeTags(ctx context.Context, input contract.MergeTagsInput) (contract.TagResponse, error)
}

const tagsPath = "/api/v1/tags"

// TagBody is the addTag request body. EntityID is omitted for the project.
type TagBody struct {
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id,omitempty"`
	Name       string `json:"name"`
}

// MergeTagBody names the tag that absorbs the one in the path.
type MergeTagBody struct {
	TargetTagID int64 `json:"target_tag_id"`
}

var (
	entityTypeParam = param{name: "entity_type", in: "query", description: "`task`, `error`, or `project`.", schema: stringSchema, required: true}
	entityIDParam   = queryParam("entity_id", "Task or error id; omitted for `project`.", idSchema)
	tagParam        = pathParam("tag", "Tag id.", idSchema)
)

func (s *Server) tagRoutes() []route {
	return []route{
		query("listTags", http.MethodGet, projectPath+"/tags", "tag.list", "Tags of a task, an error, or the project.", []param{projectParam, entityTypeParam, entityIDParam}, s.listTags),
		command("addTag", http.MethodPost, projectPath+"/tags", "tag.add", "Tag a task, an error, or the project.", []param{projectParam}, s.addTag),
		query("removeTag", http.MethodDelete, projectPath+"/tags/{tag}", "tag.remove", "Untag a task, an error, or the project; requires confirmation.", []param{
			projectParam,
			tagParam,
			entityTypeParam,
			entityIDParam,
			confirmedParam,
		}, s.removeTag),
		query("listAllTags", http.MethodGet, tagsPath, "tag.list_all", "Every tag across all projects.", nil, s.listAllTags),
		command("mergeTag", http.MethodPost, tagsPath+"/{tag}/merge", "tag.merge", "Merge a tag into a target tag across all projects.", []param{tagParam}, s.mergeTag),
	}
}

func (s *Server) addTag(r *http.Request, body TagBody) (contract.TagResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.TagResponse{}, err
	}
	return ops.AddTag(r.Context(), contract.AddTagInput{ProjectSelector: selector, EntityType: body.EntityType, EntityID: body.EntityID, TagName: body.Name})
}

func (s *Server) removeTag(r *http.Request) (contract.RemoveTagResponse, error) {
	tagID, err := parseID(r.PathValue("tag"), "tag")
	if err != nil {
		return contract.RemoveTagResponse{}, err
	}
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.RemoveTagResponse{}, err
	}
	entityID, err := tagEntityID(r)
	if err != nil {
		return contract.RemoveTagResponse{}, err
	}
	ok, err := confirmed(r)
	if err != nil {
		return contract.RemoveTagResponse{}, err
	}
	return ops.RemoveTag(r.Context(), contract.RemoveTagInput{
		ProjectSelector: selector,
		EntityType:      r.URL.Query().Get("entity_type"),
		EntityID:        entityID,
		TagID:           tagID,
		Confirmed:       ok,
	})
}

func (s *Server) listAllTags(r *http.Request) (contract.AllTagsResponse, error) {
	ops, err := s.opts.Runtimes.Global(r.Context())
	if err != nil {
		return contract.AllTagsResponse{}, err
	}
	return ops.ListAllTags(r.Context())
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

func (s *Server) mergeTag(r *http.Request, body MergeTagBody) (contract.TagResponse, error) {
	sourceID, err := parseID(r.PathValue("tag"), "tag")
	if err != nil {
		return contract.TagResponse{}, err
	}
	ops, err := s.opts.Runtimes.Global(r.Context())
	if err != nil {
		return contract.TagResponse{}, err
	}
	return ops.MergeTags(r.Context(), contract.MergeTagsInput{SourceTagID: sourceID, TargetTagID: body.TargetTagID})
}

// tagEntityID reads the optional entity_id query parameter.
func tagEntityID(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("entity_id")
	if raw == "" {
		return 0, nil
	}
	return parseID(raw, "entity_id")
}

package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listTags", http.MethodGet, "/api/v1/projects/alpha/tags?entity_type=task&entity_id=5", "", "ListTags", contract.ListTagsInput{ProjectSelector: alpha, EntityType: "task", EntityID: 5}},
	{"addTag", http.MethodPost, "/api/v1/projects/alpha/tags", `{"entity_type":"error","entity_id":3,"name":"Flaky"}`, "AddTag", contract.AddTagInput{ProjectSelector: alpha, EntityType: "error", EntityID: 3, TagName: "Flaky"}},
	{"listAllTags", http.MethodGet, "/api/v1/tags", "", "ListAllTags", nil},
})

func (f *fakeOps) ListTags(_ context.Context, in contract.ListTagsInput) (contract.TagListResponse, error) {
	f.record("ListTags", in)
	return contract.TagListResponse{}, nil
}

func (f *fakeOps) AddTag(_ context.Context, in contract.AddTagInput) (contract.TagResponse, error) {
	f.record("AddTag", in)
	return contract.TagResponse{}, nil
}

func (f *fakeOps) ListAllTags(context.Context) (contract.AllTagsResponse, error) {
	f.record("ListAllTags", nil)
	return contract.AllTagsResponse{}, nil
}

package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listTags", http.MethodGet, "/api/v1/projects/alpha/tags?entity_type=task&entity_id=5", "", "ListTags", contract.ListTagsInput{ProjectSelector: alpha, EntityType: "task", EntityID: 5}},
	{"listAllTags", http.MethodGet, "/api/v1/tags", "", "ListAllTags", nil},
})

func (f *fakeOps) ListTags(_ context.Context, in contract.ListTagsInput) (contract.TagListResponse, error) {
	f.record("ListTags", in)
	return contract.TagListResponse{}, nil
}

func (f *fakeOps) ListAllTags(context.Context) (contract.AllTagsResponse, error) {
	f.record("ListAllTags", nil)
	return contract.AllTagsResponse{}, nil
}

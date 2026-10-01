package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listTags", http.MethodGet, "/api/v1/projects/alpha/tags?entity_type=task&entity_id=5", "", "ListTags", contract.ListTagsInput{ProjectSelector: alpha, EntityType: "task", EntityID: 5}},
})

func (f *fakeOps) ListTags(_ context.Context, in contract.ListTagsInput) (contract.TagListResponse, error) {
	f.record("ListTags", in)
	return contract.TagListResponse{}, nil
}

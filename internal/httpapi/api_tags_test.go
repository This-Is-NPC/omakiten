package httpapi

import (
	"context"
	"net/http"
	"testing"

	"omakiten/internal/contract"
)

func TestRemoveTagRejectsMalformedParameters(t *testing.T) {
	for _, target := range []string{
		"/api/v1/projects/alpha/tags/x?entity_type=project",
		"/api/v1/projects/alpha/tags/9?entity_type=project&confirmed=maybe",
	} {
		t.Run(target, func(t *testing.T) {
			ops := &fakeOps{}
			server, _ := newTestServer(t, ops, fakeLog{})
			rec := do(t, server, http.MethodDelete, target, "", nil)
			if env := decode(t, rec); rec.Code != http.StatusBadRequest || env.Code != "invalid_parameter" {
				t.Fatalf("got %d %+v, want invalid_parameter", rec.Code, env)
			}
			if len(ops.calls) != 0 {
				t.Fatalf("operation ran: %+v", ops.calls)
			}
		})
	}
}

var _ = mapping([]mappingCase{
	{"listTags", http.MethodGet, "/api/v1/projects/alpha/tags?entity_type=task&entity_id=5", "", "ListTags", contract.ListTagsInput{ProjectSelector: alpha, EntityType: "task", EntityID: 5}},
	{"addTag", http.MethodPost, "/api/v1/projects/alpha/tags", `{"entity_type":"error","entity_id":3,"name":"Flaky"}`, "AddTag", contract.AddTagInput{ProjectSelector: alpha, EntityType: "error", EntityID: 3, TagName: "Flaky"}},
	{"removeTag", http.MethodDelete, "/api/v1/projects/alpha/tags/9?entity_type=task&entity_id=5&confirmed=true", "", "RemoveTag", contract.RemoveTagInput{
		ProjectSelector: alpha, EntityType: "task", EntityID: 5, TagID: 9, Confirmed: true,
	}},
	{"listAllTags", http.MethodGet, "/api/v1/tags", "", "ListAllTags", nil},
	{"mergeTag", http.MethodPost, "/api/v1/tags/4/merge", `{"target_tag_id":2}`, "MergeTags", contract.MergeTagsInput{SourceTagID: 4, TargetTagID: 2}},
})

func (f *fakeOps) ListTags(_ context.Context, in contract.ListTagsInput) (contract.TagListResponse, error) {
	f.record("ListTags", in)
	return contract.TagListResponse{}, nil
}

func (f *fakeOps) AddTag(_ context.Context, in contract.AddTagInput) (contract.TagResponse, error) {
	f.record("AddTag", in)
	return contract.TagResponse{}, nil
}

func (f *fakeOps) RemoveTag(_ context.Context, in contract.RemoveTagInput) (contract.RemoveTagResponse, error) {
	f.record("RemoveTag", in)
	return contract.RemoveTagResponse{}, nil
}

func (f *fakeOps) ListAllTags(context.Context) (contract.AllTagsResponse, error) {
	f.record("ListAllTags", nil)
	return contract.AllTagsResponse{}, nil
}

func (f *fakeOps) MergeTags(_ context.Context, in contract.MergeTagsInput) (contract.TagResponse, error) {
	f.record("MergeTags", in)
	return contract.TagResponse{}, nil
}
